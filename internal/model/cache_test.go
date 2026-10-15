package model_test

import (
	"context"
	"testing"

	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

type countingDecider struct{ calls int }

func (c *countingDecider) Decide(ctx context.Context, t model.Ticket, facts []model.KV) (model.Decision, error) {
	c.calls++
	return model.Decision{Choice: "reply", Level: "none"}, nil
}

func TestCacheHitsAcrossCalls(t *testing.T) {
	inner := &countingDecider{}
	d := model.NewCache(storetest.Graph(t)).Decider(inner, "m", "v1")
	tk := model.Ticket{ID: "t1", Body: "hello"}
	a := []model.KV{{Key: "x", Value: "1"}, {Key: "y", Value: "2"}}
	b := []model.KV{{Key: "y", Value: "2"}, {Key: "x", Value: "1"}}
	ctx := context.Background()
	for _, facts := range [][]model.KV{a, b} {
		got, err := d.Decide(ctx, tk, facts)
		if err != nil || got.Choice != "reply" {
			t.Fatalf("got %v %v", got, err)
		}
	}
	if inner.calls != 1 {
		t.Errorf("inner calls = %d, want 1", inner.calls)
	}
	if _, err := d.Decide(ctx, tk, []model.KV{{Key: "x", Value: "changed"}}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Errorf("inner calls = %d, want 2", inner.calls)
	}
}
