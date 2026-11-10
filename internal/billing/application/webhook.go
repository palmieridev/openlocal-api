package application

import "context"

// Event contains identifiers from a verified provider event. The inbound adapter
// owns signature verification, provider event decoding and relevance filtering.
type Event struct{ ID, CustomerID string }

func (s *Service) ReceiveEvent(ctx context.Context, event Event) error {
	if event.ID == "" || event.CustomerID == "" {
		return fail("invalid", "invalid_request", "invalid billing event")
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tx.SaveEventAndEnqueue(ctx, event.ID, event.CustomerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
