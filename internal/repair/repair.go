// Package repair undoes a poisoned source: it walks the provenance graph
// from the source, re-runs only the actions that depended on it, stops early
// where a re-run gives the same output, and lists effects that cannot be
// undone.
package repair

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Event is one action decided during a repair.
type Event struct {
	ActionID, Tool, TicketID, Status string
	Seq                              float64
}

// CompItem is an effect that really left the system and is now wrong.
type CompItem struct {
	EffectID, Kind, TicketID, ActionStatus string
	Amount                                 float64
	Draft                                  string
}

type Report struct {
	RepairID     string
	Counts       map[string]int
	Compensation []CompItem
}

type engine struct {
	ctx     context.Context
	g       *falkordb.Graph
	rid     string
	agents  map[string]recorder.AgentFunc
	onEvent func(Event)
	pending map[string]float64 // action id to seq; a set, so nothing runs twice
}

// Run repairs g after source turned out to be bad.
func Run(ctx context.Context, g *falkordb.Graph, source string, agents map[string]recorder.AgentFunc, d model.Drafter, onEvent func(Event)) (*Report, error) {
	e := &engine{ctx: ctx, g: g, rid: newID(), agents: agents, onEvent: onEvent, pending: map[string]float64{}}
	src := map[string]any{"src": source}

	rows, err := e.rows(store.MarkSourceBad, src)
	if err != nil {
		return nil, err
	}
	if rows[0][0].(int64) == 0 {
		return nil, fmt.Errorf("repair: graph has no source %q", source)
	}
	if err := e.exec(store.CreateRepair, map[string]any{"rid": e.rid, "src": source}); err != nil {
		return nil, err
	}
	if err := e.exec(store.Mask, src); err != nil {
		return nil, err
	}
	poisoned, err := e.rows(store.PoisonedActions, src)
	if err != nil {
		return nil, err
	}
	isPoisoned := map[string]bool{}
	for _, r := range poisoned {
		isPoisoned[r[0].(string)] = true
		e.emit(r, "poisoned")
	}
	blast, err := e.rows(store.BlastRadius, src)
	if err != nil {
		return nil, err
	}
	for _, r := range blast {
		a := r[0].(*falkordb.Node)
		if id := a.Properties["id"].(string); !isPoisoned[id] {
			e.pending[id] = a.Properties["seq"].(float64)
		}
	}

	for len(e.pending) > 0 {
		if err := e.step(e.popMin()); err != nil {
			return nil, err
		}
	}
	return e.report()
}

// step decides one pending action: skip it, or re-run it.
func (e *engine) step(id string) error {
	rows, err := e.rows(store.LoadAction, map[string]any{"id": id})
	if err != nil {
		return err
	}
	r := rows[0]
	a := recorder.ActionRef{ID: id, Tool: r[0].(string), TicketID: r[1].(string), Seq: r[2].(float64), Payload: json.RawMessage(r[3].(string))}
	if r[4] == "superseded" {
		return nil
	}
	must, err := e.rows(store.MustRerun, map[string]any{"id": id})
	if err != nil {
		return err
	}
	if must[0][0] != true {
		if err := e.exec(store.SetStatus, map[string]any{"id": id, "status": "skipped"}); err != nil {
			return err
		}
		e.event(a, "skipped")
		return nil
	}

	held, err := e.replay(a)
	if err != nil {
		return err
	}
	status := "rerun_changed"
	if held.OutputHash == r[6] {
		status = "rerun_same"
	}
	if err := e.exec(store.Reran, map[string]any{"rid": e.rid, "id": id, "status": status, "output": held.Output, "output_hash": held.OutputHash, "input_hash": held.InputHash}); err != nil {
		return err
	}
	e.event(a, status)
	if status == "rerun_same" {
		return nil // old effects and children stand
	}
	if err := e.exec(store.InvalidateEffects, map[string]any{"id": id, "rid": e.rid}); err != nil {
		return err
	}
	if err := e.writeEffects(id, held); err != nil {
		return err
	}
	return e.handoffs(a, held.Handoffs)
}

// replay re-runs a and queues every later reader of the keys it wrote.
func (e *engine) replay(a recorder.ActionRef) (*recorder.Held, error) {
	fn, ok := e.agents[a.Tool]
	if !ok {
		return nil, fmt.Errorf("repair: unknown agent %q", a.Tool)
	}
	held, err := recorder.Replay(e.ctx, e.g, a, fn)
	if err != nil {
		return nil, fmt.Errorf("repair: re-running %s %s: %w", a.Tool, a.TicketID, err)
	}
	for _, key := range held.Wrote {
		readers, err := e.rows(store.LinkLateReaders, map[string]any{"writer": a.ID, "key": key})
		if err != nil {
			return nil, err
		}
		for _, rd := range readers {
			lr, err := e.rows(store.LoadAction, map[string]any{"id": rd[0]})
			if err != nil {
				return nil, err
			}
			e.pending[rd[0].(string)] = lr[0][2].(float64)
		}
	}
	return held, nil
}

// writeEffects records what a re-run did. Repair never sends anything.
func (e *engine) writeEffects(id string, held *recorder.Held) error {
	for _, ef := range held.Effects {
		if err := recorder.WriteEffect(e.g, id, ef.Kind, ef.Amount, false); err != nil {
			return err
		}
	}
	return nil
}

// handoffs matches a re-run's handoffs to the children it had before, by
// agent and ticket. Matched children get the new payload, unmatched old ones
// are superseded, and handoffs with no old child become new actions.
func (e *engine) handoffs(parent recorder.ActionRef, hs []recorder.Job) error {
	old, err := e.rows(store.OldChildren, map[string]any{"id": parent.ID})
	if err != nil {
		return err
	}
	var pairs []any
	for _, h := range hs {
		pairs = append(pairs, []string{h.Agent, h.TicketID})
		matched := false
		for _, c := range old {
			if c[1] == h.Agent && c[2] == h.TicketID {
				if err := e.exec(store.SetPayload, map[string]any{"id": c[0], "payload": string(h.Payload)}); err != nil {
					return err
				}
				e.pending[c[0].(string)] = c[3].(float64)
				matched = true
			}
		}
		if !matched {
			if err := e.runNew(parent, h); err != nil {
				return err
			}
		}
	}
	if pairs == nil {
		pairs = []any{}
	}
	gone, err := e.rows(store.Supersede, map[string]any{"parent": parent.ID, "new_handoffs": pairs, "rid": e.rid})
	if err != nil {
		return err
	}
	for _, r := range gone {
		delete(e.pending, r[0].(string))
		e.emit(r, "superseded")
	}
	return nil
}

// runNew creates and runs an action that only exists because a re-run
// handed off differently. Its seq sits between its parent and the next one.
func (e *engine) runNew(parent recorder.ActionRef, j recorder.Job) error {
	next, err := e.rows(store.NextSeq, map[string]any{"seq": parent.Seq})
	if err != nil {
		return err
	}
	seq := parent.Seq + 1
	if n, ok := next[0][0].(float64); ok {
		seq = (parent.Seq + n) / 2
	}
	a, err := recorder.Start(e.g, j, seq)
	if err != nil {
		return err
	}
	held, err := e.replay(a)
	if err != nil {
		return err
	}
	if err := e.exec(store.Created, map[string]any{"rid": e.rid, "id": a.ID, "output": held.Output, "output_hash": held.OutputHash, "input_hash": held.InputHash}); err != nil {
		return err
	}
	e.event(a, "new")
	if err := e.writeEffects(a.ID, held); err != nil {
		return err
	}
	return e.handoffs(a, held.Handoffs)
}

func (e *engine) report() (*Report, error) {
	rep := &Report{RepairID: e.rid, Counts: map[string]int{}}
	counts, err := e.rows(store.StatusCounts, nil)
	if err != nil {
		return nil, err
	}
	for _, r := range counts {
		rep.Counts[r[0].(string)] = int(r[1].(int64))
	}
	comp, err := e.rows(store.Compensation, map[string]any{"rid": e.rid})
	if err != nil {
		return nil, err
	}
	for _, r := range comp {
		rep.Compensation = append(rep.Compensation, CompItem{EffectID: r[0].(string), Kind: r[1].(string), TicketID: r[2].(string), ActionStatus: r[3].(string), Amount: r[4].(float64)})
	}
	return rep, nil
}

func (e *engine) popMin() string {
	var id string
	for k, s := range e.pending {
		if id == "" || s < e.pending[id] {
			id = k
		}
	}
	delete(e.pending, id)
	return id
}

func (e *engine) event(a recorder.ActionRef, status string) {
	if e.onEvent != nil {
		e.onEvent(Event{ActionID: a.ID, Tool: a.Tool, TicketID: a.TicketID, Status: status, Seq: a.Seq})
	}
}

// emit sends an event for a row shaped (id, tool, ticket_id, seq).
func (e *engine) emit(r []any, status string) {
	e.event(recorder.ActionRef{ID: r[0].(string), Tool: r[1].(string), TicketID: r[2].(string), Seq: r[3].(float64)}, status)
}

func (e *engine) exec(q string, params map[string]any) error {
	_, err := e.g.Query(q, params, nil)
	return err
}

func (e *engine) rows(q string, params map[string]any) ([][]any, error) {
	res, err := e.g.Query(q, params, nil)
	if err != nil {
		return nil, err
	}
	var out [][]any
	for res.Next() {
		out = append(out, res.Record().Values())
	}
	return out, nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
