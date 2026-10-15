package model

import (
	"context"
	"fmt"
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
