// Command kintsugi runs the scenario, repairs a poisoned graph and compares
// it with a clean run.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/repair"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

const usage = `usage:
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
	case len(args) == 2 && args[0] == "scenario" && (args[1] == "clean" || args[1] == "demo"):
		return runScenario(ctx, db, args[1])
	case len(args) == 3 && args[0] == "repair":
		return runRepair(ctx, db, args[1], args[2])
	case len(args) == 3 && args[0] == "diff":
		return runDiff(db, args[1], args[2])
	}
	return fmt.Errorf("%s", usage)
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
	if err := scenario.Run(ctx, g, name == "demo", m.w, m.d); err != nil {
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
		fmt.Printf("\n%s %s %.2f (%s)\n%s\n", c.TicketID, c.Kind, c.Amount, c.ActionStatus, c.Draft)
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
