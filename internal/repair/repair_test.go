package repair_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRepairDraftsCompensation(t *testing.T) {
	demo := build(t, true)
	rep, err := repair.Run(context.Background(), demo, scenario.PoisonSource, fakeAgents(), model.FakeDrafter{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Compensation {
		if c.Draft == "" {
			t.Errorf("no draft for %s %s", c.Kind, c.TicketID)
		}
	}
	if n := count(t, demo, "MATCH (:Effect)-[:COMPENSATED_BY]->(d:Draft) WHERE d.text <> '' RETURN count(d)"); n != 10 {
		t.Errorf("drafts in graph: %d, want 10", n)
	}
	if n := count(t, demo, "MATCH (d:Draft {kind:'refund_reversal'}) RETURN count(d)"); n != 5 {
		t.Errorf("refund reversals: %d, want 5", n)
	}
	// 39 from the run, plus 6 written by repair (t04 refund and email, 4 reply emails). Drafts add none.
	if n := count(t, demo, "MATCH (e:Effect) RETURN count(e)"); n != 45 {
		t.Errorf("effects: %d, want 45", n)
	}
	if n := count(t, demo, "MATCH (e:Effect {sent: true}) RETURN count(e)"); n != 39 {
		t.Errorf("sent effects: %d, want 39", n)
	}
}

func TestDiffKeepsColumnsApart(t *testing.T) {
	a, b := storetest.Graph(t), storetest.Graph(t)
	if _, err := a.Query("CREATE (:Fact {key: 'a', value: 'b | c', seq: 1.0, active: true})", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Query("CREATE (:Fact {key: 'a | b', value: 'c', seq: 1.0, active: true})", nil, nil); err != nil {
		t.Fatal(err)
	}
	diff, err := repair.Diff(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 2 {
		t.Errorf("diff = %v, want 2 rows", diff)
	}
}

// Two new siblings from one re-run must keep handoff order, so the second
// sees what the first wrote, as it would in a normal run.
func TestNewSiblingsKeepOrder(t *testing.T) {
	g := storetest.Graph(t)
	set := func(src, v string) recorder.AgentFunc {
		return func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			if err := r.Ingest(src, "web", src); err != nil {
				return err
			}
			return r.Set("k", v)
		}
	}
	ags := map[string]recorder.AgentFunc{
		"good": set("good-src", "clean"),
		"bad":  set("bad-src", "bad"),
		"p": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, _, err := r.Get("k")
			if err != nil {
				return err
			}
			if err := r.Finish(map[string]any{"k": v}); err != nil || v != "clean" {
				return err
			}
			if err := r.Handoff("x", struct{}{}); err != nil {
				return err
			}
			return r.Handoff("y", struct{}{})
		},
		"x": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error { return r.Set("m", "from x") },
		"y": func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
			v, _, err := r.Get("m")
			if err != nil {
				return err
			}
			return r.Finish(map[string]any{"saw": v})
		},
	}
	q := recorder.NewQueue(g, ags)
	for _, a := range []string{"good", "bad", "p"} {
		q.Enqueue(recorder.Job{Agent: a, TicketID: "t1", Payload: json.RawMessage(`{}`)})
	}
	if err := q.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.Run(context.Background(), g, "bad-src", ags, model.FakeDrafter{}, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(t, g, "MATCH (x:Action {tool:'x', status:'new'}), (y:Action {tool:'y', status:'new'}) WHERE x.seq < y.seq RETURN count(*)"); n != 1 {
		t.Error("new sibling y does not come after x")
	}
	res, err := g.Query("MATCH (y:Action {tool:'y'}) RETURN y.output", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Next()
	if v, _ := res.Record().GetByIndex(0); v != `{"saw":"from x"}` {
		t.Errorf("y output = %v, want it to see x's write", v)
	}
}

type failingDrafter struct{}

func (failingDrafter) Draft(context.Context, string, model.Ticket, string, string) (string, error) {
	return "", errors.New("quota")
}

func TestDraftFailureKeepsReport(t *testing.T) {
	demo := build(t, true)
	rep, err := repair.Run(context.Background(), demo, scenario.PoisonSource, fakeAgents(), failingDrafter{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Compensation) != 10 {
		t.Fatalf("compensation: %d items, want 10", len(rep.Compensation))
	}
	for _, c := range rep.Compensation {
		if c.DraftErr == "" {
			t.Errorf("%s %s: no draft error recorded", c.Kind, c.TicketID)
		}
	}
}

func TestSecondRepairRefused(t *testing.T) {
	demo := build(t, true)
	if _, err := repair.Run(context.Background(), demo, scenario.PoisonSource, fakeAgents(), model.FakeDrafter{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.Run(context.Background(), demo, scenario.PoisonSource, fakeAgents(), model.FakeDrafter{}, nil); err == nil {
		t.Error("want an error repairing a graph that was already repaired")
	}
}
