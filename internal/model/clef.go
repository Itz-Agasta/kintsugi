package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ClefDecider asks a System One decision model (Clef on Workers AI, or Jev)
// for the triage choice. URL is the full endpoint, so switching to Jev is a
// config change.
type ClefDecider struct {
	URL     string
	Token   string
	Model   string        // "clef-flash" or "clef"
	Backoff time.Duration // first wait after a 429, doubled each retry; 0 means 1s
}

// WorkersAIURL is the REST endpoint for a Clef model on Workers AI.
func WorkersAIURL(accountID, model string) string {
	return "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/ai/run/@cf/cloudflare/" + model
}

// ClefPromptVersion is part of the cache key. Bump it when the questions change.
const ClefPromptVersion = "triage-v1"

var levels = []string{"none", "partial", "full"}

func (c *ClefDecider) Decide(ctx context.Context, t Ticket, facts []KV) (Decision, error) {
	policies := map[string]string{}
	for _, f := range facts {
		policies[f.Key] = f.Value
	}
	// needs_human is left out on purpose: the model has to judge from the body.
	state := map[string]any{
		"policies": policies,
		"ticket": map[string]any{
			"customer":         t.Customer,
			"claims_vip":       t.ClaimsVIP,
			"account_tier":     t.AccountTier,
			"days_since_order": t.DaysSinceOrder,
			"defective":        t.Defective,
			"order_amount":     t.OrderAmount,
			"message":          t.Body,
		},
	}
	req := map[string]any{
		"model": c.Model,
		"state": state,
		"questions": map[string]any{
			"choice": map[string]any{
				"type":         "choice",
				"instructions": "Apply the support policies in the state to this ticket. What should support do?",
				"criteria": map[string]string{
					"refund":   "Refund the customer, as the policies allow",
					"reply":    "Reply to the customer without a refund",
					"escalate": "Hand the ticket to a human: fraud, missing items, contracts or anything the policies do not cover",
				},
			},
			"level": map[string]any{
				"type":         "score",
				"instructions": "If support refunds, how much of the order do the policies allow?",
				"criteria":     levels,
			},
		},
	}
	var ans struct {
		Answers struct {
			Choice struct{ Choice string }                    `json:"choice"`
			Level  struct{ Probabilities map[string]float64 } `json:"level"`
		} `json:"answers"`
	}
	if err := c.post(ctx, req, &ans); err != nil {
		return Decision{}, err
	}
	d := Decision{Choice: ans.Answers.Choice.Choice, Level: "none"}
	if d.Choice == "refund" {
		best := -1.0
		for i, l := range levels {
			if p := ans.Answers.Level.Probabilities[strconv.Itoa(i)]; p > best {
				best, d.Level = p, l
			}
		}
	}
	return d, nil
}

// post sends the request, retrying on 429, and unwraps the Workers AI
// {"result": ...} envelope when there is one.
func (c *ClefDecider) post(ctx context.Context, req any, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	wait := c.Backoff
	if wait == 0 {
		wait = time.Second
	}
	for attempt := 0; ; attempt++ {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		r.Header.Set("Content-Type", "application/json")
		if c.Token != "" {
			r.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			wait *= 2
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("clef: HTTP %d: %s", resp.StatusCode, raw)
		}
		var env struct{ Result json.RawMessage }
		if json.Unmarshal(raw, &env) == nil && env.Result != nil {
			raw = env.Result
		}
		return json.Unmarshal(raw, out)
	}
}
