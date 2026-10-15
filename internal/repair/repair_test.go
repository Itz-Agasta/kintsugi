package repair_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/repair"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func build(t *testing.T, poisoned bool) *falkordb.Graph {
	t.Helper()
	g := storetest.Graph(t)
	if err := scenario.Run(context.Background(), g, poisoned, model.FakeWriter{}, model.FakeDecider{}); err != nil {
		t.Fatal(err)
	}
	return g
}

func count(t *testing.T, g *falkordb.Graph, q string) int64 {
	t.Helper()
	res, err := g.Query(q, nil, nil)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	res.Next()
	v, _ := res.Record().GetByIndex(0)
	return v.(int64)
}

func fakeAgents() map[string]recorder.AgentFunc {
	return agents.All(model.FakeWriter{}, model.FakeDecider{})
}

func TestRepairMatchesClean(t *testing.T) {
	clean, demo := build(t, false), build(t, true)
	var events []repair.Event
	rep, err := repair.Run(context.Background(), demo, scenario.PoisonSource, fakeAgents(), model.FakeDrafter{}, func(e repair.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"poisoned": 1, "rerun_same": 3, "skipped": 6, "rerun_changed": 7, "superseded": 8, "new": 4}
	for s, n := range want {
		if rep.Counts[s] != n {
			t.Errorf("%s: %d, want %d", s, rep.Counts[s], n)
		}
	}
	if len(rep.Counts) != len(want) {
		t.Errorf("counts = %v", rep.Counts)
	}
	if len(rep.Compensation) != 10 {
		t.Errorf("compensation: %d items, want 10", len(rep.Compensation))
	}
	if len(events) != 29 {
		t.Errorf("events: %d, want 29 (one per decided action)", len(events))
	}
	if n := count(t, demo, "MATCH (e:Effect {live: true}) RETURN count(e)"); n != 35 {
		t.Errorf("live effects: %d, want 35", n)
	}
	if n := count(t, demo, "MATCH (:Action {tool:'triage', ticket_id:'t05'})-[:HANDED_OFF]->(:Action {tool:'notifier', status:'new'}) RETURN count(*)"); n != 1 {
		t.Errorf("t05 new notifier: %d, want 1", n)
	}
	diff, err := repair.Diff(clean, demo)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Errorf("diff not empty:\n%s", strings.Join(diff, "\n"))
	}
}

func TestRepairUnknownSource(t *testing.T) {
	demo := build(t, true)
	if _, err := repair.Run(context.Background(), demo, "no/such/source", fakeAgents(), model.FakeDrafter{}, nil); err == nil {
		t.Error("want an error for a source the graph never ingested")
	}
}

func TestDiffFindsMismatch(t *testing.T) {
	diff, err := repair.Diff(build(t, false), build(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(diff, "\n"), "vip_rules") {
		t.Errorf("diff does not mention vip_rules:\n%s", strings.Join(diff, "\n"))
	}
}

func TestDiffCountsDuplicates(t *testing.T) {
	a, b := build(t, false), build(t, false)
	if _, err := b.Query("MATCH (a:Action {tool:'refund', ticket_id:'t01'}) CREATE (a)-[:CAUSED]->(:Effect {kind:'refund', ticket_id:'t01', amount: 120.0, live: true, sent: true})", nil, nil); err != nil {
		t.Fatal(err)
	}
	diff, err := repair.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 1 {
		t.Errorf("diff = %v, want the one duplicate refund", diff)
	}
}
