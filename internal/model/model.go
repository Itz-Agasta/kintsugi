// Package model holds the model interfaces the agents call, a rule-based
// fake, and a response cache stored in FalkorDB.
package model

import "context"

type KV struct{ Key, Value string }

type Page struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Text string `json:"text"`
}

type Ticket struct {
	ID             string  `json:"id"`
	Customer       string  `json:"customer"`
	ClaimsVIP      bool    `json:"claims_vip"`
	AccountTier    string  `json:"account_tier"`
	DaysSinceOrder int     `json:"days_since_order"`
	Defective      bool    `json:"defective"`
	NeedsHuman     bool    `json:"needs_human"`
	OrderAmount    float64 `json:"order_amount"`
	Body           string  `json:"body"`
}

// Decision is a typed triage choice: refund, reply or escalate, with a
// refund level of none, partial or full.
type Decision struct {
	Choice string `json:"choice"`
	Level  string `json:"level"`
}

// Writer turns a page into facts (the research agent's model).
type Writer interface {
	Facts(ctx context.Context, p Page) ([]KV, error)
}

// Decider makes the triage choice for a ticket given the facts it read.
type Decider interface {
	Decide(ctx context.Context, t Ticket, facts []KV) (Decision, error)
}
