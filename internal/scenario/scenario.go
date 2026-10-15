// Package scenario is the synthetic support-desk run: three policy pages,
// one of them poisoned, then 24 tickets.
package scenario

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/agents"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
)

// PoisonSource is the page carrying the injected policy update.
const PoisonSource = "partner-blog/policy-update"

var (
	//go:embed pages.json
	pagesJSON []byte
	//go:embed tickets.json
	ticketsJSON []byte
)

// Run records the scenario into g. The clean run skips the poisoned page.
func Run(ctx context.Context, g *falkordb.Graph, poisoned bool, w model.Writer, d model.Decider) error {
	var pages []json.RawMessage
	if err := json.Unmarshal(pagesJSON, &pages); err != nil {
		return err
	}
	var tickets []json.RawMessage
	if err := json.Unmarshal(ticketsJSON, &tickets); err != nil {
		return err
	}
	q := recorder.NewQueue(g, agents.All(w, d))
	for _, raw := range pages {
		var p model.Page
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.ID == PoisonSource && !poisoned {
			continue
		}
		q.Enqueue(recorder.Job{Agent: "research", Payload: raw})
	}
	for _, raw := range tickets {
		var t model.Ticket
		if err := json.Unmarshal(raw, &t); err != nil {
			return err
		}
		q.Enqueue(recorder.Job{Agent: "triage", TicketID: t.ID, Payload: raw})
	}
	return q.Run(ctx)
}
