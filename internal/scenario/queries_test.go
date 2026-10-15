package scenario_test

import (
	"strings"
	"testing"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/scenario"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// These run the Day 2 repair queries once against a recorded demo graph, to
// prove FalkorDB accepts them and gives the answers the data implies.

func query(t *testing.T, g *falkordb.Graph, name, q string, params map[string]any) []*falkordb.Record {
	t.Helper()
	res, err := g.Query(q, params, nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out []*falkordb.Record
	for res.Next() {
		out = append(out, res.Record())
	}
	return out
}

func actionID(t *testing.T, g *falkordb.Graph, tool, ticket string) string {
	t.Helper()
	r := query(t, g, "lookup", "MATCH (a:Action {tool: $tool, ticket_id: $ticket}) RETURN a.id", map[string]any{"tool": tool, "ticket": ticket})
	if len(r) != 1 {
		t.Fatalf("%s %s: %d actions", tool, ticket, len(r))
	}
	v, _ := r[0].GetByIndex(0)
	return v.(string)
}

func TestDay2QueriesRun(t *testing.T) {
	g := build(t, true)
	src := map[string]any{"src": scenario.PoisonSource}

	// 1 research, plus triage, refund and notifier for each of the 8 VIP tickets.
	if n := len(query(t, g, "BlastRadius", store.BlastRadius, src)); n != 25 {
		t.Errorf("BlastRadius: %d actions, want 25", n)
	}

	poison := query(t, g, "poison action", "MATCH (:Source {id: $src})<-[:INGESTED]-(a:Action) RETURN a.id", src)
	writer, _ := poison[0].GetByIndex(0)
	late := query(t, g, "LinkLateReaders", store.LinkLateReaders, map[string]any{"writer": writer, "key": "vip_rules"})
	if len(late) != 8 {
		t.Errorf("LinkLateReaders: %d readers, want 8", len(late))
	}

	query(t, g, "Mask", store.Mask, src)
	if n := len(query(t, g, "masked facts", "MATCH (f:Fact {key: 'vip_rules', active: false}) RETURN f", nil)); n != 1 {
		t.Errorf("Mask: %d inactive vip_rules facts, want 1", n)
	}
	read := query(t, g, "ReadLatestActive", store.ReadLatestActive, map[string]any{"key": "vip_rules", "reader_seq": 100.0})
	if v, _ := read[0].GetByIndex(0); !strings.Contains(v.(*falkordb.Node).Properties["value"].(string), "partial refund") {
		t.Errorf("ReadLatestActive after Mask: %v, want the clean vip_rules", v)
	}

	must := query(t, g, "MustRerun", store.MustRerun, map[string]any{"id": actionID(t, g, "triage", "t01")})
	if v, _ := must[0].GetByIndex(0); v != true {
		t.Errorf("MustRerun on t01 triage: %v, want true", v)
	}
	must = query(t, g, "MustRerun", store.MustRerun, map[string]any{"id": actionID(t, g, "triage", "t09")})
	if v, _ := must[0].GetByIndex(0); v != false {
		t.Errorf("MustRerun on t09 triage: %v, want false", v)
	}

	path := query(t, g, "ExplainPath", store.ExplainPath, map[string]any{"src": scenario.PoisonSource, "aid": actionID(t, g, "notifier", "t05")})
	if len(path) != 1 {
		t.Fatalf("ExplainPath: %d rows", len(path))
	}
	if p, _ := path[0].GetByIndex(0); p.(falkordb.Path).EdgeCount() != 4 {
		t.Errorf("ExplainPath: %d edges, want 4", p.(falkordb.Path).EdgeCount())
	}

	query(t, g, "Supersede", store.Supersede, map[string]any{
		"parent":       actionID(t, g, "triage", "t05"),
		"new_handoffs": []any{[]string{"notifier", "t05"}},
		"rid":          "r1",
	})
	if n := len(query(t, g, "superseded", "MATCH (a:Action {ticket_id: 't05', status: 'superseded'}) RETURN a", nil)); n != 2 {
		t.Errorf("Supersede: %d superseded actions on t05, want 2 (refund, notifier)", n)
	}
	if n := len(query(t, g, "Compensation", store.Compensation, map[string]any{"rid": "r1"})); n != 2 {
		t.Errorf("Compensation: %d effects, want 2 (refund, email)", n)
	}

	if n := len(query(t, g, "DiffFacts", store.DiffFacts, nil)); n != 2 {
		t.Errorf("DiffFacts: %d keys, want 2", n)
	}
	if n := len(query(t, g, "DiffDecisions", store.DiffDecisions, nil)); n != 24 {
		t.Errorf("DiffDecisions: %d tickets, want 24", n)
	}
	if n := len(query(t, g, "DiffEffects", store.DiffEffects, nil)); n != 37 {
		t.Errorf("DiffEffects: %d live effects, want 37 (39 minus the 2 superseded on t05)", n)
	}
}
