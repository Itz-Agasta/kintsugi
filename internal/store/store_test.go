package store_test

import (
	"testing"

	"github.com/Itz-Agasta/kintsugi/internal/store/storetest"
)

func TestResetCreatesIndexes(t *testing.T) {
	g := storetest.Graph(t)
	res, err := g.Query("CALL db.indexes() YIELD label RETURN label", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for res.Next() {
		v, _ := res.Record().GetByIndex(0)
		got[v.(string)] = true
	}
	for _, l := range []string{"Action", "Fact", "Source", "Agent", "Effect"} {
		if !got[l] {
			t.Errorf("no index on %s, have %v", l, got)
		}
	}
}
