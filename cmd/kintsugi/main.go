// Command kintsugi runs the scenario into FalkorDB.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

const usage = "usage: kintsugi scenario <clean|demo>"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 || args[0] != "scenario" || (args[1] != "clean" && args[1] != "demo") {
		return fmt.Errorf(usage)
	}
	name := args[1]
	db, err := store.Connect()
	if err != nil {
		return err
	}
	g, err := store.Reset(db, name)
	if err != nil {
		return err
	}
	ctx := context.Background()
	w, d, err := models(ctx, model.NewCache(db.SelectGraph("llm_cache")))
	if err != nil {
		return err
	}
	if err := scenario.Run(ctx, g, name == "demo", w, d); err != nil {
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

// models uses Gemini and Clef when their keys are set, the fakes otherwise.
// Both go through the shared response cache.
func models(ctx context.Context, cache *model.Cache) (model.Writer, model.Decider, error) {
	var w model.Writer = model.FakeWriter{}
	wName, wPrompt := "fake", "v1"
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		gw, err := model.NewGeminiWriter(ctx, key, model.GeminiModel())
		if err != nil {
			return nil, nil, err
		}
		w, wName, wPrompt = gw, model.GeminiModel(), model.GeminiPromptVersion
	}
	var d model.Decider = model.FakeDecider{}
	dName, dPrompt := "fake", "v1"
	if acc, tok := os.Getenv("CF_ACCOUNT_ID"), os.Getenv("CF_API_TOKEN"); acc != "" && tok != "" {
		m := cmp.Or(os.Getenv("CLEF_MODEL"), "clef-flash")
		d, dName, dPrompt = &model.ClefDecider{URL: model.WorkersAIURL(acc, m), Token: tok, Model: m}, m, model.ClefPromptVersion
	}
	fmt.Fprintf(os.Stderr, "research: %s, triage: %s\n", wName, dName)
	return cache.Writer(w, wName, wPrompt), cache.Decider(d, dName, dPrompt), nil
}
