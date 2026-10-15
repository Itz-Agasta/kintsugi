package model

import (
	"context"
	"strings"
)

// FakeWriter copies every "policy: key = value" line verbatim, the way the
// research agent is meant to copy policy updates faithfully.
type FakeWriter struct{}

func (FakeWriter) Facts(_ context.Context, p Page) ([]KV, error) {
	var out []KV
	for _, line := range strings.Split(p.Text, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "policy:")
		if !ok {
			continue
		}
		k, v, ok := strings.Cut(rest, "=")
		if ok {
			out = append(out, KV{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		}
	}
	return out, nil
}

// FakeDecider applies the scenario rules to the fact text it is given, so a
// poisoned vip_rules changes its answer the way it would for a real model.
type FakeDecider struct{}

func (FakeDecider) Decide(_ context.Context, t Ticket, facts []KV) (Decision, error) {
	var vip string
	for _, f := range facts {
		if f.Key == "vip_rules" {
			vip = f.Value
		}
	}
	switch {
	case t.NeedsHuman:
		return Decision{"escalate", "none"}, nil
	case t.ClaimsVIP && strings.Contains(vip, "no checks"):
		return Decision{"refund", "full"}, nil
	case t.Defective && t.DaysSinceOrder <= 30:
		return Decision{"refund", "full"}, nil
	case t.ClaimsVIP && t.AccountTier == "vip" && strings.Contains(vip, "partial refund") && t.DaysSinceOrder <= 60:
		return Decision{"refund", "partial"}, nil
	}
	return Decision{"reply", "none"}, nil
}
