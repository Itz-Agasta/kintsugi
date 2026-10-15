package repair

import (
	"fmt"
	"strings"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Diff compares the latest active facts, the final decision per ticket and
// the live effects per ticket between two graphs. Ids, versions, seq and
// sent are never compared, since they differ between a clean run and a
// repaired one. An empty result means the graphs agree.
func Diff(clean, demo *falkordb.Graph) ([]string, error) {
	var out []string
	for _, q := range []struct{ name, cypher string }{
		{"facts", store.DiffFacts},
		{"decisions", store.DiffDecisions},
		{"effects", store.DiffEffects},
	} {
		a, err := lines(clean, q.cypher)
		if err != nil {
			return nil, err
		}
		b, err := lines(demo, q.cypher)
		if err != nil {
			return nil, err
		}
		// Rows are compared as multisets, so a duplicated effect is a difference.
		n := map[string]int{}
		for _, l := range a {
			n[l]++
		}
		for _, l := range b {
			n[l]--
		}
		for _, l := range append(a, b...) {
			for ; n[l] > 0; n[l]-- {
				out = append(out, fmt.Sprintf("%s only in %s: %s", q.name, clean.Id, l))
			}
			for ; n[l] < 0; n[l]++ {
				out = append(out, fmt.Sprintf("%s only in %s: %s", q.name, demo.Id, l))
			}
		}
	}
	return out, nil
}

func lines(g *falkordb.Graph, q string) ([]string, error) {
	res, err := g.Query(q, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for res.Next() {
		var cols []string
		for _, v := range res.Record().Values() {
			cols = append(cols, fmt.Sprintf("%q", fmt.Sprint(v))) // quoted so a " | " inside a value cannot merge columns
		}
		out = append(out, strings.Join(cols, " | "))
	}
	return out, nil
}
