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
