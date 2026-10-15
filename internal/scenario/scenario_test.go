package scenario_test

import (
	"context"
	"slices"
	"testing"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func build(t *testing.T, poisoned bool) *falkordb.Graph {
	t.Helper()
	g := storetest.Graph(t)
	if err := scenario.Run(context.Background(), g, poisoned, model.FakeWriter{}, model.FakeDecider{}, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

func rows(t *testing.T, g *falkordb.Graph, q string) []any {
	t.Helper()
	res, err := g.Query(q, nil, nil)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	var out []any
	for res.Next() {
		v, _ := res.Record().GetByIndex(0)
		out = append(out, v)
	}
	return out
}

func count(t *testing.T, g *falkordb.Graph, q string) int64 {
	t.Helper()
	return rows(t, g, q)[0].(int64)
}

func TestDemoShape(t *testing.T) {
	g := build(t, true)
	if n := count(t, g, "MATCH (a:Action {tool:'research'}) RETURN count(a)"); n != 3 {
		t.Errorf("research actions: %d", n)
	}
	if n := count(t, g, "MATCH (a:Action {tool:'triage'}) RETURN count(a)"); n != 24 {
		t.Errorf("triage actions: %d", n)
	}
	fed := rows(t, g, `MATCH (:Source {id:'partner-blog/policy-update'})<-[:INGESTED]-(:Action)-[:FEEDS]->(a:Action {tool:'triage'})
		RETURN a.ticket_id ORDER BY a.ticket_id`)
	want := []any{"t01", "t02", "t03", "t04", "t05", "t06", "t07", "t08"}
	if !slices.Equal(fed, want) {
		t.Errorf("poison feeds %v, want %v", fed, want)
	}
	if n := count(t, g, `MATCH (:Source {id:'partner-blog/policy-update'})<-[:INGESTED]-(:Action)-[:FEEDS|HANDED_OFF*0..]->(a:Action)
		WHERE a.ticket_id >= 't09' RETURN count(a)`); n != 0 {
		t.Errorf("%d actions on t09..t24 reachable from poison", n)
	}
}

func TestCleanShape(t *testing.T) {
	g := build(t, false)
	if n := count(t, g, "MATCH (a:Action {tool:'research'}) RETURN count(a)"); n != 2 {
		t.Errorf("research actions: %d", n)
	}
	if n := count(t, g, "MATCH (s:Source {id:'partner-blog/policy-update'}) RETURN count(s)"); n != 0 {
		t.Errorf("poison source present")
	}
}

func TestRunTwiceSameCounts(t *testing.T) {
	db := storetest.DB(t)
	g := build(t, true)
	first := count(t, g, "MATCH (a:Action) RETURN count(a)")
	g, err := store.Reset(db, g.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := scenario.Run(context.Background(), g, true, model.FakeWriter{}, model.FakeDecider{}, nil); err != nil {
		t.Fatal(err)
	}
	if second := count(t, g, "MATCH (a:Action) RETURN count(a)"); second != first {
		t.Errorf("first run %d actions, second %d", first, second)
	}
}

func TestRunReportsEachAction(t *testing.T) {
	g := storetest.Graph(t)
	seen := map[string]bool{}
	after := func(a recorder.ActionRef) {
		if seen[a.ID] {
			t.Errorf("action %s reported twice", a.ID)
		}
		seen[a.ID] = true
	}
	if err := scenario.Run(context.Background(), g, true, model.FakeWriter{}, model.FakeDecider{}, after); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 66 {
		t.Errorf("reported %d actions, want 66", len(seen))
	}
}
