package recorder

import (
	"context"

	"github.com/FalkorDB/falkordb-go/v2"
)

type HeldEffect struct {
	Kind   string
	Amount float64
}

// Held is what a replayed action produced. Reads and fact writes go to the
// graph as usual; effects, handoffs and the output wait here so the repair
// engine can decide what to keep once it has compared hashes.
type Held struct {
	Output, OutputHash, InputHash string
	Effects                       []HeldEffect
	Handoffs                      []Job
	Wrote                         []string
}

// Replay runs fn again as action a, at a's seq.
func Replay(ctx context.Context, g *falkordb.Graph, a ActionRef, fn AgentFunc) (*Held, error) {
	r := &Rec{ID: a.ID, Seq: a.Seq, g: g, job: Job{Agent: a.Tool, TicketID: a.TicketID, Payload: a.Payload}, held: &Held{}}
	if err := fn(ctx, r, a.Payload); err != nil {
		return nil, err
	}
	if !r.finished {
		if err := r.Finish(map[string]any{}); err != nil {
			return nil, err
		}
	}
	return r.held, nil
}
