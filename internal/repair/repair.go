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
	"slices"
	"time"

	"github.com/FalkorDB/falkordb-go/v2"
	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Event is one action decided during a repair. MustRerun and QueryMs are set
// for actions the MustRerun query decided (re-run or skipped): its answer and
// FalkorDB's internal execution time for it. StartMs is when the decision
// began, counted from the start of the repair, and DurMs how long it took.
type Event struct {
	ActionID  string  `json:"action_id"`
	Tool      string  `json:"tool"`
	TicketID  string  `json:"ticket_id"`
	Status    string  `json:"status"`
	Seq       float64 `json:"seq"`
	MustRerun *bool   `json:"must_rerun,omitempty"`
	QueryMs   float64 `json:"query_ms,omitempty"`
	StartMs   float64 `json:"start_ms"`
	DurMs     float64 `json:"dur_ms"`
}

// decision is what an event reports about how its action was decided.
type decision struct {
	start time.Time
	must  *bool
	ms    float64
}

// CompItem is an effect that really left the system and is now wrong.
type CompItem struct {
	EffectID     string  `json:"effect_id"`
	Kind         string  `json:"kind"`
	TicketID     string  `json:"ticket_id"`
	ActionStatus string  `json:"action_status"`
	Amount       float64 `json:"amount"`
	Draft        string  `json:"draft"`
	DraftErr     string  `json:"draft_error,omitempty"`
}

type Report struct {
	RepairID     string         `json:"repair_id"`
	Counts       map[string]int `json:"counts"`
	Compensation []CompItem     `json:"compensation"`
}

type engine struct {
	ctx     context.Context
	g       *falkordb.Graph
	rid     string
	agents  map[string]recorder.AgentFunc
	onEvent func(Event)
	t0      time.Time
	pending map[string]float64 // action id to seq; a set, so nothing runs twice
}

// Run repairs g after source turned out to be bad.
func Run(ctx context.Context, g *falkordb.Graph, source string, agents map[string]recorder.AgentFunc, d model.Drafter, onEvent func(Event)) (*Report, error) {
	e := &engine{ctx: ctx, g: g, rid: newID(), agents: agents, onEvent: onEvent, t0: time.Now(), pending: map[string]float64{}}
	src := map[string]any{"src": source}

	done, err := e.rows(store.RepairCount, nil)
	if err != nil {
		return nil, err
	}
	if done[0][0].(int64) > 0 {
		return nil, fmt.Errorf("repair: graph %s was already repaired; reset it and run again", g.Id)
	}
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
	blast, err := e.rows(store.BlastRadius, src)
	if err != nil {
		return nil, err
	}
	for _, r := range blast {
		a := r[0].(*falkordb.Node)
		e.pending[a.Properties["id"].(string)] = a.Properties["seq"].(float64)
	}
	// A poisoned action never should have run: what it sent is now wrong, and
	// what it handed off to only exists because of it.
	for _, r := range poisoned {
		id := r[0].(string)
		delete(e.pending, id)
		e.emit(r, "poisoned")
		if err := e.exec(store.InvalidateEffects, map[string]any{"id": id, "rid": e.rid}); err != nil {
			return nil, err
		}
		if err := e.supersede(id, []any{}); err != nil {
			return nil, err
		}
	}

	for len(e.pending) > 0 {
		if err := e.step(e.popMin()); err != nil {
			return nil, err
		}
	}
	return e.report(d)
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
	d := decision{start: time.Now()}
	res, err := e.g.Query(store.MustRerun, map[string]any{"id": id}, nil)
	if err != nil {
		return err
	}
	res.Next()
	must := res.Record().Values()[0] == true
	d.must, d.ms = &must, res.InternalExecutionTime()
	if !must {
		if err := e.exec(store.SetStatus, map[string]any{"id": id, "status": "skipped"}); err != nil {
			return err
		}
		e.event(a, "skipped", d)
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
	e.event(a, status, d)
	if status == "rerun_same" {
		return nil // old effects and children stand
	}
	if err := e.exec(store.InvalidateEffects, map[string]any{"id": id, "rid": e.rid}); err != nil {
		return err
	}
	if err := e.writeEffects(id, held); err != nil {
		return err
	}
	_, err = e.handoffs(a, held.Handoffs)
	return err
}

// replay re-runs a and queues every later reader of the keys it wrote.
func (e *engine) replay(a recorder.ActionRef) (*recorder.Held, error) {
	fn, ok := e.agents[a.Tool]
	if !ok {
		return nil, fmt.Errorf("repair: unknown agent %q", a.Tool)
	}
	before, err := e.rows(store.OwnKeys, map[string]any{"id": a.ID})
	if err != nil {
		return nil, err
	}
	held, err := recorder.Replay(e.ctx, e.g, a, fn)
	if err != nil {
		return nil, fmt.Errorf("repair: re-running %s %s: %w", a.Tool, a.TicketID, err)
	}
	// Keys written before but not this time are retired, and their readers
	// re-run without them.
	touched := held.Wrote
	for _, r := range before {
		key := r[0].(string)
		if slices.Contains(held.Wrote, key) {
			continue
		}
		if err := e.exec(store.DeactivateOwn, map[string]any{"aid": a.ID, "key": key}); err != nil {
			return nil, err
		}
		touched = append(touched, key)
	}
	for _, key := range touched {
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
// are superseded, and handoffs with no old child become new actions. It
// returns the highest seq given to a new action, so later siblings go after.
func (e *engine) handoffs(parent recorder.ActionRef, hs []recorder.Job) (float64, error) {
	last := parent.Seq
	old, err := e.rows(store.OldChildren, map[string]any{"id": parent.ID})
	if err != nil {
		return 0, err
	}
	// Each handoff takes at most one old child with the same agent and
	// ticket, so duplicates match one to one.
	keep := []any{}
	taken := map[string]bool{}
	var unmatched []recorder.Job
	for _, h := range hs {
		matched := false
		for _, c := range old {
			id := c[0].(string)
			if taken[id] || c[1] != h.Agent || c[2] != h.TicketID {
				continue
			}
			if err := e.exec(store.SetPayload, map[string]any{"id": id, "payload": string(h.Payload)}); err != nil {
				return 0, err
			}
			e.pending[id] = c[3].(float64)
			taken[id] = true
			keep = append(keep, id)
			matched = true
			break
		}
		if !matched {
			unmatched = append(unmatched, h)
		}
	}
	// Supersede before creating new actions: they are children of the same
	// parent and are not in keep.
	if err := e.supersede(parent.ID, keep); err != nil {
		return 0, err
	}
	for _, h := range unmatched {
		if last, err = e.runNew(h, last); err != nil {
			return 0, err
		}
	}
	return last, nil
}

// supersede retires the HANDED_OFF children of parent that are not in keep,
// and everything below them.
func (e *engine) supersede(parent string, keep []any) error {
	gone, err := e.rows(store.Supersede, map[string]any{"parent": parent, "keep": keep, "rid": e.rid})
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
// handed off differently. It gets a seq between after (its parent or the
// last new sibling) and the next existing action, and returns the highest
// seq used by it and anything it handed off to.
func (e *engine) runNew(j recorder.Job, after float64) (float64, error) {
	next, err := e.rows(store.NextSeq, map[string]any{"seq": after})
	if err != nil {
		return 0, err
	}
	seq := after + 1
	if n, ok := next[0][0].(float64); ok {
		seq = (after + n) / 2
	}
	a, err := recorder.Start(e.g, j, seq)
	if err != nil {
		return 0, err
	}
	held, err := e.replay(a)
	if err != nil {
		return 0, err
	}
	if err := e.exec(store.Created, map[string]any{"rid": e.rid, "id": a.ID, "output": held.Output, "output_hash": held.OutputHash, "input_hash": held.InputHash}); err != nil {
		return 0, err
	}
	e.event(a, "new", decision{start: time.Now()})
	if err := e.writeEffects(a.ID, held); err != nil {
		return 0, err
	}
	return e.handoffs(a, held.Handoffs)
}

func (e *engine) report(d model.Drafter) (*Report, error) {
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
		c := CompItem{EffectID: r[0].(string), Kind: r[1].(string), TicketID: r[2].(string), ActionStatus: r[3].(string), Amount: r[4].(float64)}
		// The repair is already in the graph; a failed draft must not hide the list.
		if c.Draft, err = e.draft(d, c); err != nil {
			c.DraftErr = err.Error()
		}
		rep.Compensation = append(rep.Compensation, c)
	}
	return rep, nil
}

// draft writes and stores the follow-up for one compensation item: a
// correction email for an email, a reversal request for a refund.
func (e *engine) draft(d model.Drafter, c CompItem) (string, error) {
	kind := "correction_email"
	if c.Kind == "refund" {
		kind = "refund_reversal"
	}
	rows, err := e.rows(store.CompContext, map[string]any{"eid": c.EffectID, "rid": e.rid})
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("repair: %d triage decisions for ticket %s, want 1", len(rows), c.TicketID)
	}
	var t model.Ticket
	if err := json.Unmarshal([]byte(rows[0][2].(string)), &t); err != nil {
		return "", err
	}
	text, err := d.Draft(e.ctx, kind, t, rows[0][0].(string), rows[0][1].(string))
	if err != nil {
		return "", err
	}
	return text, e.exec(store.SaveDraft, map[string]any{"eid": c.EffectID, "kind": kind, "text": text})
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

func (e *engine) event(a recorder.ActionRef, status string, d decision) {
	if e.onEvent == nil {
		return
	}
	ms := func(t time.Duration) float64 { return float64(t.Microseconds()) / 1000 }
	sent := time.Now()
	e.onEvent(Event{
		ActionID: a.ID, Tool: a.Tool, TicketID: a.TicketID, Status: status, Seq: a.Seq,
		MustRerun: d.must, QueryMs: d.ms,
		StartMs: ms(d.start.Sub(e.t0)), DurMs: ms(sent.Sub(d.start)),
	})
	// Time spent in the callback (the server paces its stream there) is not
	// repair work, so the clock skips it.
	e.t0 = e.t0.Add(time.Since(sent))
}

// emit sends an event for a row shaped (id, tool, ticket_id, seq).
func (e *engine) emit(r []any, status string) {
	e.event(recorder.ActionRef{ID: r[0].(string), Tool: r[1].(string), TicketID: r[2].(string), Seq: r[3].(float64)}, status, decision{start: time.Now()})
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
