package model

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// Drafter writes the follow-up for an effect that already left the system:
// a correction email or a refund reversal request. Drafts are never sent.
type Drafter interface {
	Draft(ctx context.Context, kind string, t Ticket, wrong, right string) (string, error)
}

type FakeDrafter struct{}

func (FakeDrafter) Draft(_ context.Context, kind string, t Ticket, wrong, right string) (string, error) {
	return fmt.Sprintf("%s for %s: we sent %s, it should have been %s.", kind, t.ID, wrong, right), nil
}

// GeminiDraftPromptVersion is part of the cache key. Bump it when the prompt changes.
const GeminiDraftPromptVersion = "draft-v1"

// GeminiDrafter writes follow-up drafts with Gemini.
type GeminiDrafter struct {
	client *genai.Client
	model  string
}

func NewGeminiDrafter(ctx context.Context, apiKey, model string) (*GeminiDrafter, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, err
	}
	return &GeminiDrafter{client: c, model: model}, nil
}

var draftPrompts = map[string]string{
	"correction_email": "Write a short email from a support desk to the customer below. An earlier automated message to them was based on a wrong policy. Say plainly what was wrong and what the correct outcome is. If they were told they would get a refund they are not owed, say so honestly. At most 5 sentences, plain text, no subject line, no filler.",
	"refund_reversal":  "Write a short internal request from the support desk to the finance team to reverse or adjust a refund that was issued under a wrong policy. Give the ticket id, the amount that went out, and the correct outcome. At most 4 sentences, plain text, no filler.",
}

func (d *GeminiDrafter) Draft(ctx context.Context, kind string, t Ticket, wrong, right string) (string, error) {
	p, ok := draftPrompts[kind]
	if !ok {
		return "", fmt.Errorf("draft: unknown kind %q", kind)
	}
	prompt := fmt.Sprintf("%s\n\nTicket %s from %s, order amount %.2f.\nTheir message: %s\nWhat the system did (wrong): %s\nCorrect decision: %s\n",
		p, t.ID, t.Customer, t.OrderAmount, t.Body, wrong, right)
	resp, err := d.client.Models.GenerateContent(ctx, d.model, genai.Text(prompt), &genai.GenerateContentConfig{Temperature: genai.Ptr[float32](0)})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text()), nil
}
