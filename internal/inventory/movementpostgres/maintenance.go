package movementpostgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

var _ movement.MaintenanceStore = (*Store)(nil)
var _ movement.ChangeTransaction = (*changeTransaction)(nil)

type changeTransaction struct{ *transaction }

func (s *Store) BeginChange(ctx context.Context) (movement.ChangeTransaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &changeTransaction{&transaction{tx: tx, q: s.q.WithTx(tx)}}, nil
}

func (t *changeTransaction) LockMovement(ctx context.Context, businessID, id uuid.UUID) (movement.Movement, error) {
	item, err := t.q.GetStockMovementForUpdate(ctx, db.GetStockMovementForUpdateParams{ID: id, BusinessID: businessID})
	return mapMovement(item), changeError(err)
}

func (t *changeTransaction) RequireVariant(ctx context.Context, businessID, id uuid.UUID) error {
	_, err := t.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: id, BusinessID: businessID})
	return changeError(err)
}

func (t *changeTransaction) RequireLocation(ctx context.Context, businessID, id uuid.UUID) error {
	_, err := t.q.GetInventoryLocationForBusiness(ctx, db.GetInventoryLocationForBusinessParams{ID: id, BusinessID: businessID})
	return changeError(err)
}

func (t *changeTransaction) StockLevel(ctx context.Context, data movement.Data) (movement.StockLevel, error) {
	level, err := t.q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID})
	return mapLevel(level), changeError(err)
}

func (t *changeTransaction) ApplyNonnegativeDelta(ctx context.Context, data movement.Data, delta decimal.Decimal) (movement.StockLevel, error) {
	// A zero delta preserves the legacy no-op behavior even when creation has
	// allowed negative stock. It must not refresh the stock timestamp.
	if delta.IsZero() {
		return t.StockLevel(ctx, data)
	}
	level, err := t.q.ApplyNonnegativeStockDelta(ctx, db.ApplyNonnegativeStockDeltaParams{
		BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID, QuantityOnHand: delta,
	})
	if errors.Is(err, pgx.ErrNoRows) && delta.IsPositive() {
		level, err = t.q.InsertNonnegativeStockDelta(ctx, db.InsertNonnegativeStockDeltaParams{
			BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID, QuantityOnHand: delta,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return movement.StockLevel{}, movement.ErrNegativeStock
	}
	return mapLevel(level), err
}

func (t *changeTransaction) Update(ctx context.Context, item movement.Movement) (movement.Movement, error) {
	updated, err := t.q.UpdateStockMovement(ctx, db.UpdateStockMovementParams{
		ID: item.ID, BusinessID: item.BusinessID, LocationID: item.LocationID,
		MovementType: item.MovementType, Quantity: item.Quantity, UnitCost: nullDecimal(item.UnitCost),
		ReferenceType: nullString(item.ReferenceType), ReferenceID: nullString(item.ReferenceID), Notes: item.Notes,
		IdempotencyKey: nullString(&item.IdempotencyKey),
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return movement.Movement{}, movement.ErrConflict
	}
	return mapMovement(updated), changeError(err)
}

func (t *changeTransaction) Delete(ctx context.Context, businessID, id uuid.UUID) error {
	_, err := t.q.DeleteStockMovement(ctx, db.DeleteStockMovementParams{ID: id, BusinessID: businessID})
	return changeError(err)
}

func (t *changeTransaction) AuditChange(ctx context.Context, input movement.DeleteInput, action string) error {
	_, err := t.q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		BusinessID: uuid.NullUUID{UUID: input.BusinessID, Valid: true}, ActorUserID: uuid.NullUUID{UUID: input.ActorID, Valid: true},
		Action: action, EntityType: "stock_movement", EntityID: uuid.NullUUID{UUID: input.MovementID, Valid: true}, Metadata: []byte(`{}`),
	})
	return err
}

func changeError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return movement.ErrNotFound
	}
	return err
}
