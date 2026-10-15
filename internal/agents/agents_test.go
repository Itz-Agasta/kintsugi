package agents_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func TestRefundRejectsLevelsOtherThanPartialOrFull(t *testing.T) {
	for _, level := range []string{"none", "", "half"} {
		g := storetest.Graph(t)
		q := recorder.NewQueue(g, agents.All(model.FakeWriter{}, model.FakeDecider{}))
		payload, _ := json.Marshal(agents.Outcome{Ticket: model.Ticket{ID: "t1", OrderAmount: 100}, Choice: "refund", Level: level})
		q.Enqueue(recorder.Job{Agent: "refund", TicketID: "t1", Payload: payload})
		if err := q.Run(context.Background()); err == nil {
			t.Errorf("level %q: want an error", level)
		}
		res, err := g.Query("MATCH (e:Effect) RETURN count(e)", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Next()
		if n, _ := res.Record().GetByIndex(0); n.(int64) != 0 {
			t.Errorf("level %q: %v effects recorded, want 0", level, n)
		}
	}
}

type oneFact string

func (v oneFact) Facts(context.Context, model.Page) ([]model.KV, error) {
	return []model.KV{{Key: "vip_rules", Value: string(v)}}, nil
}

func TestResearchHashCoversValues(t *testing.T) {
	var hashes []any
	for _, v := range []oneFact{"clean rule", "poisoned rule"} {
		g := storetest.Graph(t)
		q := recorder.NewQueue(g, agents.All(v, model.FakeDecider{}))
		q.Enqueue(recorder.Job{Agent: "research", Payload: json.RawMessage(`{"id":"p","url":"u","text":""}`)})
		if err := q.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		res, err := g.Query("MATCH (a:Action {tool:'research'}) RETURN a.output_hash", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Next()
		h, _ := res.Record().GetByIndex(0)
		hashes = append(hashes, h)
	}
	if hashes[0] == hashes[1] {
		t.Error("research output_hash is the same for different fact values")
	}
}
