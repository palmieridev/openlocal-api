package movementpostgres

import (
	"context"

	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

var _ movement.ReadStore = (*Store)(nil)

func (s *Store) ListMovements(ctx context.Context, input movement.ListInput) ([]movement.Movement, error) {
	items, err := s.q.ListStockMovements(ctx, db.ListStockMovementsParams{BusinessID: input.BusinessID, Limit: input.Limit, Offset: input.Offset})
	if err != nil {
		return nil, err
	}
	result := make([]movement.Movement, 0, len(items))
	for _, item := range items {
		result = append(result, mapMovement(item))
	}
	return result, nil
}

func (s *Store) ListLevels(ctx context.Context, input movement.ListInput) ([]movement.StockLevel, error) {
	items, err := s.q.ListStockLevels(ctx, db.ListStockLevelsParams{BusinessID: input.BusinessID, Limit: input.Limit, Offset: input.Offset})
	if err != nil {
		return nil, err
	}
	result := make([]movement.StockLevel, 0, len(items))
	for _, item := range items {
		result = append(result, mapLevel(item))
	}
	return result, nil
}
