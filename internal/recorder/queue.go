// Package recorder is the only way agents touch memory. Every action, read,
// write, handoff and effect is written to the FalkorDB graph as it happens.
package recorder

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// AgentFunc runs one job. It reads and writes memory only through r.
type AgentFunc func(ctx context.Context, r *Rec, payload json.RawMessage) error

// Job is one unit of agent work. ParentID becomes a HANDED_OFF edge.
type Job struct {
	Agent    string
	TicketID string
	ParentID string
	Payload  json.RawMessage
}

// Queue runs jobs for one graph, one at a time, in FIFO order. That order
// is the seq order, so it is also a valid re-execution order.
type Queue struct {
	g      *falkordb.Graph
	agents map[string]AgentFunc
	jobs   []Job
	seq    float64
}

func NewQueue(g *falkordb.Graph, agents map[string]AgentFunc) *Queue {
	return &Queue{g: g, agents: agents}
}

func (q *Queue) Enqueue(j Job) { q.jobs = append(q.jobs, j) }

// Run drains the queue and stops at the first error.
func (q *Queue) Run(ctx context.Context) error {
	for len(q.jobs) > 0 {
		j := q.jobs[0]
		q.jobs = q.jobs[1:]
		if err := q.runOne(ctx, j); err != nil {
			return fmt.Errorf("%s job at seq %v: %w", j.Agent, q.seq, err)
		}
	}
	return nil
}

func (q *Queue) runOne(ctx context.Context, j Job) error {
	fn, ok := q.agents[j.Agent]
	if !ok {
		return fmt.Errorf("unknown agent %q", j.Agent)
	}
	q.seq++
	r := &Rec{ID: newID(), Seq: q.seq, q: q, job: j}
	if err := r.query(store.StartAction, map[string]any{"agent": j.Agent, "id": r.ID, "ticket": j.TicketID, "seq": r.Seq}); err != nil {
		return err
	}
	if j.ParentID != "" {
		if err := r.query(store.LinkHandoff, map[string]any{"parent": j.ParentID, "id": r.ID}); err != nil {
			return err
		}
	}
	if err := fn(ctx, r, j.Payload); err != nil {
		return err
	}
	if !r.finished {
		return r.Finish(map[string]any{})
	}
	return nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
