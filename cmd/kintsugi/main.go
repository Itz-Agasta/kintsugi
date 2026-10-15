// Command kintsugi runs the scenario, repairs a poisoned graph and compares
// it with a clean run.
package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/repair"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/server"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

const usage = `usage:
  kintsugi serve
  kintsugi scenario <clean|demo>
  kintsugi repair <graph> <source>
  kintsugi diff <graph> <graph>`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx := context.Background()
	db, err := store.Connect()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 1 && args[0] == "serve":
		return runServe(ctx, db)
	case len(args) == 2 && args[0] == "scenario" && (args[1] == "clean" || args[1] == "demo"):
		return runScenario(ctx, db, args[1])
	case len(args) == 3 && args[0] == "repair":
		return runRepair(ctx, db, args[1], args[2])
	case len(args) == 3 && args[0] == "diff":
		return runDiff(db, args[1], args[2])
	}
	return fmt.Errorf("%s", usage)
}

func runServe(ctx context.Context, db *falkordb.FalkorDB) error {
	m, err := models(ctx, db)
	if err != nil {
		return err
	}
	cfg := server.Config{
		CleanGraph:   "clean",
		Prefix:       "demo-",
		RunPace:      envMillis("RUN_PACE_MS", 120),
		RepairPace:   envMillis("REPAIR_PACE_MS", 250),
		Idle:         time.Duration(envInt("SESSION_IDLE_MIN", 60)) * time.Minute,
		MaxSessions:  envInt("MAX_SESSIONS", 20),
		AllowOrigins: strings.Split(cmp.Or(os.Getenv("ALLOW_ORIGINS"), "http://localhost:5173"), ","),
	}
	s := server.New(db, m.w, m.d, m.dr, cfg)
	if err := s.Start(ctx); err != nil {
		return err
	}
	addr := ":" + cmp.Or(os.Getenv("PORT"), "8080")
	fmt.Fprintf(os.Stderr, "listening on %s\n", addr)
	// No write timeout: the run and repair streams stay open for a while.
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func envMillis(key string, def int) time.Duration {
	return time.Duration(envInt(key, def)) * time.Millisecond
}

func runScenario(ctx context.Context, db *falkordb.FalkorDB, name string) error {
	g, err := store.Reset(db, name)
	if err != nil {
		return err
	}
	m, err := models(ctx, db)
	if err != nil {
		return err
	}
	if err := scenario.Run(ctx, g, name == "demo", m.w, m.d, nil); err != nil {
		return err
	}
	res, err := g.Query(store.Counts, nil, nil)
	if err != nil {
		return err
	}
	res.Next()
	r := res.Record()
	a, _ := r.Get("actions")
	f, _ := r.Get("facts")
	e, _ := r.Get("effects")
	fmt.Printf("%s: %v actions, %v facts, %v effects\n", name, a, f, e)
	return nil
}

func runRepair(ctx context.Context, db *falkordb.FalkorDB, graph, source string) error {
	m, err := models(ctx, db)
	if err != nil {
		return err
	}
	rep, err := repair.Run(ctx, db.SelectGraph(graph), source, agents.All(m.w, m.d), m.dr, nil)
	if err != nil {
		return err
	}
	fmt.Printf("repair %s of %s from %s\n", rep.RepairID, graph, source)
	var statuses []string
	for s := range rep.Counts {
		statuses = append(statuses, s)
	}
	slices.Sort(statuses)
	for _, s := range statuses {
		fmt.Printf("  %-14s %d\n", s, rep.Counts[s])
	}
	fmt.Printf("\ncannot be undone: %d\n", len(rep.Compensation))
	for _, c := range rep.Compensation {
		draft := c.Draft
		if c.DraftErr != "" {
			draft = "draft failed: " + c.DraftErr
		}
		fmt.Printf("\n%s %s %.2f (%s)\n%s\n", c.TicketID, c.Kind, c.Amount, c.ActionStatus, draft)
	}
	return nil
}

func runDiff(db *falkordb.FalkorDB, a, b string) error {
	diff, err := repair.Diff(db.SelectGraph(a), db.SelectGraph(b))
	if err != nil {
		return err
	}
	for _, l := range diff {
		fmt.Println(l)
	}
	if len(diff) > 0 {
		return fmt.Errorf("%s and %s differ in %d rows", a, b, len(diff))
	}
	fmt.Printf("%s and %s match: facts, decisions and live effects\n", a, b)
	return nil
}

type modelSet struct {
	w  model.Writer
	d  model.Decider
	dr model.Drafter
}

// models uses Gemini and Clef when their keys are set, the fakes otherwise.
// All of them go through the shared response cache.
func models(ctx context.Context, db *falkordb.FalkorDB) (modelSet, error) {
	cache := model.NewCache(db.SelectGraph("llm_cache"))
	var w model.Writer = model.FakeWriter{}
	var dr model.Drafter = model.FakeDrafter{}
	wName, wPrompt, drPrompt := "fake", "v1", "v1"
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		gw, err := model.NewGeminiWriter(ctx, key, model.GeminiModel())
		if err != nil {
			return modelSet{}, err
		}
		gd, err := model.NewGeminiDrafter(ctx, key, model.GeminiModel())
		if err != nil {
			return modelSet{}, err
		}
		w, dr, wName, wPrompt, drPrompt = gw, gd, model.GeminiModel(), model.GeminiPromptVersion, model.GeminiDraftPromptVersion
	}
	var d model.Decider = model.FakeDecider{}
	dName, dPrompt := "fake", "v1"
	if acc, tok := os.Getenv("CF_ACCOUNT_ID"), os.Getenv("CF_API_TOKEN"); acc != "" && tok != "" {
		m := cmp.Or(os.Getenv("CLEF_MODEL"), "clef-flash")
		d, dName, dPrompt = &model.ClefDecider{URL: model.WorkersAIURL(acc, m), Token: tok, Model: m}, m, model.ClefPromptVersion
	}
	fmt.Fprintf(os.Stderr, "research and drafts: %s, triage: %s\n", wName, dName)
	return modelSet{
		w:  cache.Writer(w, wName, wPrompt),
		d:  cache.Decider(d, dName, dPrompt),
		dr: cache.Drafter(dr, wName, drPrompt),
	}, nil
}
