package movement

import (
	"context"

	"github.com/google/uuid"
)

// ListInput scopes reads to the caller-authorized business. Inventory's list
// contract supports business and pagination filters only.
type ListInput struct {
	BusinessID    uuid.UUID
	Limit, Offset int32
}

type ReadStore interface {
	ListMovements(context.Context, ListInput) ([]Movement, error)
	ListLevels(context.Context, ListInput) ([]StockLevel, error)
}

type Reader struct{ store ReadStore }

func NewReader(store ReadStore) *Reader { return &Reader{store: store} }

func (s *Reader) Movements(ctx context.Context, input ListInput) ([]Movement, error) {
	input, err := listPage(input)
	if err != nil {
		return nil, err
	}
	return s.store.ListMovements(ctx, input)
}

func (s *Reader) Levels(ctx context.Context, input ListInput) ([]StockLevel, error) {
	input, err := listPage(input)
	if err != nil {
		return nil, err
	}
	return s.store.ListLevels(ctx, input)
}

func listPage(input ListInput) (ListInput, error) {
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 {
		return ListInput{}, invalid("limit must be between 1 and 100")
	}
	if input.Offset < 0 || input.Offset > 2_000_000_000 {
		return ListInput{}, invalid("offset must be between 0 and 2000000000")
	}
	return input, nil
}
