// Command kintsugi runs the scenario into FalkorDB.
package main

import (
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
	cache := model.NewCache(db.SelectGraph("llm_cache"))
	w := cache.Writer(model.FakeWriter{}, "fake", "v1")
	d := cache.Decider(model.FakeDecider{}, "fake", "v1")
	if err := scenario.Run(context.Background(), g, name == "demo", w, d); err != nil {
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
