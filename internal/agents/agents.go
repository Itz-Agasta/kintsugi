// Package agents holds the four support-desk agents. They are plain
// functions that touch memory only through the recorder.
package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Itz-Agasta/kintsugi/internal/model"
	"github.com/Itz-Agasta/kintsugi/internal/recorder"
)

// Outcome is what triage and refund hand to the next agent.
type Outcome struct {
	Ticket model.Ticket `json:"ticket"`
	Choice string       `json:"choice"`
	Level  string       `json:"level"`
	Amount float64      `json:"amount"`
}

func All(w model.Writer, d model.Decider) map[string]recorder.AgentFunc {
	return map[string]recorder.AgentFunc{
		"research": research(w),
		"triage":   triage(d),
		"refund":   refund,
		"notifier": notifier,
	}
}

// research ingests one page and stores the policy facts it finds.
func research(w model.Writer) recorder.AgentFunc {
	return func(ctx context.Context, r *recorder.Rec, payload json.RawMessage) error {
		var p model.Page
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if err := r.Ingest(p.ID, "web", p.URL); err != nil {
			return err
		}
		facts, err := w.Facts(ctx, p)
		if err != nil {
			return err
		}
		// Values are in the output so a changed policy changes the hash.
		written := map[string]string{}
		for _, f := range facts {
			if err := r.Set(f.Key, f.Value); err != nil {
				return err
			}
			written[f.Key] = f.Value
		}
		return r.Finish(map[string]any{"facts": written})
	}
}

// triage decides refund, reply or escalate. It reads vip_rules only for
// tickets that claim VIP status.
func triage(d model.Decider) recorder.AgentFunc {
	return func(ctx context.Context, r *recorder.Rec, payload json.RawMessage) error {
		var t model.Ticket
		if err := json.Unmarshal(payload, &t); err != nil {
			return err
		}
		keys := []string{"refund_policy"}
		if t.ClaimsVIP {
			keys = append(keys, "vip_rules")
		}
		var facts []model.KV
		for _, k := range keys {
			v, ok, err := r.Get(k)
			if err != nil {
				return err
			}
			if ok {
				facts = append(facts, model.KV{Key: k, Value: v})
			}
		}
		dec, err := d.Decide(ctx, t, facts)
		if err != nil {
			return err
		}
		if err := r.Finish(map[string]any{"choice": dec.Choice, "level": dec.Level}); err != nil {
			return err
		}
		next := "notifier"
		if dec.Choice == "refund" {
			next = "refund"
		}
		return r.Handoff(next, Outcome{Ticket: t, Choice: dec.Choice, Level: dec.Level})
	}
}

// refund pays out to the sandbox ledger, then hands off to the notifier.
func refund(ctx context.Context, r *recorder.Rec, payload json.RawMessage) error {
	var o Outcome
	if err := json.Unmarshal(payload, &o); err != nil {
		return err
	}
	switch o.Level {
	case "full":
		o.Amount = o.Ticket.OrderAmount
	case "partial":
		o.Amount = o.Ticket.OrderAmount / 2
	default:
		return fmt.Errorf("refund for %s: level %q is not partial or full", o.Ticket.ID, o.Level)
	}
	if err := r.Effect("refund", o.Amount); err != nil {
		return err
	}
	if err := r.Finish(map[string]any{"amount": o.Amount}); err != nil {
		return err
	}
	return r.Handoff("notifier", o)
}

// notifier emails the customer through the fake outbox.
func notifier(ctx context.Context, r *recorder.Rec, payload json.RawMessage) error {
	var o Outcome
	if err := json.Unmarshal(payload, &o); err != nil {
		return err
	}
	if err := r.Effect("email", 0); err != nil {
		return err
	}
	return r.Finish(map[string]any{"template": o.Choice, "level": o.Level, "amount": o.Amount})
}
