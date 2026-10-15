package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Itz-Agasta/kintsugi/internal/store"
)

// Rec is the handle an agent gets for the one action it is running.
type Rec struct {
	ID  string
	Seq float64

	q        *Queue
	job      Job
	reads    map[string]string
	finished bool
}

func (r *Rec) query(q string, params map[string]any) error {
	_, err := r.q.g.Query(q, params, nil)
	return err
}

// Ingest records that this action read outside input.
func (r *Rec) Ingest(id, kind, url string) error {
	return r.query(store.Ingest, map[string]any{"aid": r.ID, "sid": id, "kind": kind, "url": url})
}

// Get returns the latest active value of key written before this action.
func (r *Rec) Get(key string) (string, bool, error) {
	res, err := r.q.g.Query(store.Get, map[string]any{"key": key, "seq": r.Seq, "aid": r.ID}, nil)
	if err != nil {
		return "", false, err
	}
	if !res.Next() {
		return "", false, nil
	}
	v, _ := res.Record().GetByIndex(0)
	s := v.(string)
	if r.reads == nil {
		r.reads = map[string]string{}
	}
	r.reads[key] = s
	return s, true, nil
}

// Set writes a new version of key.
func (r *Rec) Set(key, value string) error {
	return r.query(store.Set, map[string]any{"aid": r.ID, "id": newID(), "key": key, "value": value})
}

// Effect records something that left the system. Sending is simulated:
// the Effect node is the outbox and the ledger.
func (r *Rec) Effect(kind string, amount float64) error {
	return r.query(store.Effect, map[string]any{"aid": r.ID, "id": newID(), "kind": kind, "amount": amount})
}

// Handoff queues a job for another agent with this action as its parent.
func (r *Rec) Handoff(agent string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r.q.Enqueue(Job{Agent: agent, TicketID: r.job.TicketID, ParentID: r.ID, Payload: b})
	return nil
}

// Finish stores the decision and its hash. The hash covers only the
// decision fields, so equal decisions hash equal and repair can stop early.
func (r *Rec) Finish(decision map[string]any) error {
	out, err := json.Marshal(decision) // map keys come out sorted
	if err != nil {
		return err
	}
	r.finished = true
	return r.query(store.Finish, map[string]any{
		"aid":         r.ID,
		"output":      string(out),
		"output_hash": hash(string(out)),
		"input_hash":  hash(r.inputs()),
	})
}

// inputs is the payload plus every key/value read, sorted by key.
func (r *Rec) inputs() string {
	keys := make([]string, 0, len(r.reads))
	for k := range r.reads {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.Write(r.job.Payload)
	for _, k := range keys {
		b.WriteString("\n" + k + "=" + r.reads[k])
	}
	return b.String()
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
