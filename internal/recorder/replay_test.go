package recorder_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func TestStartStoresPayload(t *testing.T) {
	g := storetest.Graph(t)
	a, err := recorder.Start(g, recorder.Job{Agent: "x", TicketID: "t1", Payload: json.RawMessage(`{"a":1}`)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, g, "MATCH (a:Action {id:'"+a.ID+"'}) RETURN a.payload"); got != `{"a":1}` {
		t.Errorf("payload = %v", got)
	}
	if a.Tool != "x" || a.TicketID != "t1" || a.Seq != 1 {
		t.Errorf("ref = %+v", a)
	}
}

func TestReplayHoldsEffectsAndHandoffs(t *testing.T) {
	g := storetest.Graph(t)
	a, err := recorder.Start(g, recorder.Job{Agent: "x", TicketID: "t1", Payload: json.RawMessage(`{}`)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	held, err := recorder.Replay(context.Background(), g, a, func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
		if err := r.Set("k", "v"); err != nil {
			return err
		}
		if err := r.Effect("refund", 5); err != nil {
			return err
		}
		return r.Handoff("notifier", map[string]int{"n": 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Effects) != 1 || held.Effects[0] != (recorder.HeldEffect{Kind: "refund", Amount: 5}) {
		t.Errorf("effects = %v", held.Effects)
	}
	if len(held.Handoffs) != 1 || held.Handoffs[0].ParentID != a.ID || held.Handoffs[0].Agent != "notifier" || held.Handoffs[0].TicketID != "t1" {
		t.Errorf("handoffs = %+v", held.Handoffs)
	}
	if len(held.Wrote) != 1 || held.Wrote[0] != "k" {
		t.Errorf("wrote = %v", held.Wrote)
	}
	if n := count(t, g, "MATCH (e:Effect) RETURN count(e)"); n != 0 {
		t.Errorf("%d effects written, want 0", n)
	}
	if s := scalar(t, g, "MATCH (f:Fact {key:'k'}) RETURN f.seq"); s != 3.0 {
		t.Errorf("fact seq = %v, want 3", s)
	}
}

func TestReplayDeactivatesOwnOldVersion(t *testing.T) {
	g := storetest.Graph(t)
	val := "v1"
	set := func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error { return r.Set("k", val) }
	q := recorder.NewQueue(g, map[string]recorder.AgentFunc{"w": set})
	q.Enqueue(recorder.Job{Agent: "w", Payload: json.RawMessage(`{}`)})
	if err := q.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := scalar(t, g, "MATCH (a:Action) RETURN a.id").(string)
	val = "v2"
	if _, err := recorder.Replay(context.Background(), g, recorder.ActionRef{ID: id, Tool: "w", Seq: 1, Payload: json.RawMessage(`{}`)}, set); err != nil {
		t.Fatal(err)
	}
	if n := count(t, g, "MATCH (f:Fact {key:'k', active:true}) RETURN count(f)"); n != 1 {
		t.Errorf("%d active k facts, want 1", n)
	}
	if v := scalar(t, g, "MATCH (f:Fact {key:'k', active:true}) RETURN f.value"); v != "v2" {
		t.Errorf("active value = %v", v)
	}
}

func TestReplayFinishDoesNotTouchNode(t *testing.T) {
	g := storetest.Graph(t)
	out := "a"
	fin := func(ctx context.Context, r *recorder.Rec, _ json.RawMessage) error {
		return r.Finish(map[string]any{"x": out})
	}
	q := recorder.NewQueue(g, map[string]recorder.AgentFunc{"w": fin})
	q.Enqueue(recorder.Job{Agent: "w", Payload: json.RawMessage(`{}`)})
	if err := q.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := scalar(t, g, "MATCH (a:Action) RETURN a.output_hash")
	id := scalar(t, g, "MATCH (a:Action) RETURN a.id").(string)
	out = "b"
	held, err := recorder.Replay(context.Background(), g, recorder.ActionRef{ID: id, Tool: "w", Seq: 1, Payload: json.RawMessage(`{}`)}, fin)
	if err != nil {
		t.Fatal(err)
	}
	if after := scalar(t, g, "MATCH (a:Action) RETURN a.output_hash"); after != before {
		t.Error("Replay overwrote output_hash")
	}
	if held.Output != `{"x":"b"}` || held.OutputHash == before || held.InputHash == "" {
		t.Errorf("held = %+v", held)
	}
}
