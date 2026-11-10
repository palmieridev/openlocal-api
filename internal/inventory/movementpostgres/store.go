// Package movementpostgres adapts the movement ports to sqlc and pgx.
package movementpostgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool: pool, q: q} }

var _ movement.Store = (*Store)(nil)
var _ movement.Transaction = (*transaction)(nil)

func (s *Store) RequireVariant(ctx context.Context, businessID, variantID uuid.UUID) error {
	_, err := s.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{ID: variantID, BusinessID: businessID})
	return err
}
func (s *Store) Begin(ctx context.Context) (movement.Transaction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &transaction{tx: tx, q: s.q.WithTx(tx)}, nil
}
func (s *Store) FindMovement(ctx context.Context, businessID uuid.UUID, key string) (movement.Movement, error) {
	item, err := s.q.GetStockMovementByIdempotencyKey(ctx, db.GetStockMovementByIdempotencyKeyParams{BusinessID: businessID, IdempotencyKey: sql.NullString{String: key, Valid: true}})
	return mapMovement(item), err
}
func (s *Store) StockLevel(ctx context.Context, data movement.Data) (movement.StockLevel, error) {
	level, err := s.q.GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID})
	return mapLevel(level), err
}

type transaction struct {
	tx pgx.Tx
	q  *db.Queries
}

func (t *transaction) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *transaction) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
func (t *transaction) RequireLocation(ctx context.Context, businessID, locationID uuid.UUID) error {
	_, err := t.q.GetInventoryLocationForBusiness(ctx, db.GetInventoryLocationForBusinessParams{ID: locationID, BusinessID: businessID})
	if errors.Is(err, pgx.ErrNoRows) {
		return movement.ErrLocationNotFound
	}
	return err
}
func (t *transaction) DefaultLocation(ctx context.Context, businessID uuid.UUID) (uuid.UUID, error) {
	location, err := t.q.GetDefaultInventoryLocation(ctx, businessID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Concurrent inserts can conflict on either the default-per-business
		// index or (business_id, name). Wait for either winner without aborting
		// the transaction, then read it in a fresh READ COMMITTED snapshot.
		location, err = t.q.TryCreateDefaultInventoryLocation(ctx, businessID)
		if errors.Is(err, pgx.ErrNoRows) {
			location, err = t.q.GetDefaultInventoryLocation(ctx, businessID)
		}
	}
	return location.ID, err
}
func (t *transaction) Insert(ctx context.Context, data movement.Data) (movement.Movement, error) {
	item, err := t.q.CreateStockMovement(ctx, Params(data))
	if errors.Is(err, pgx.ErrNoRows) {
		return movement.Movement{}, movement.ErrDuplicate
	}
	return mapMovement(item), err
}
func (t *transaction) ApplyDelta(ctx context.Context, data movement.Data, delta decimal.Decimal) (movement.StockLevel, error) {
	level, err := t.q.ApplyStockDelta(ctx, db.ApplyStockDeltaParams{BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID, QuantityOnHand: delta})
	return mapLevel(level), err
}
func (t *transaction) AuditCreated(ctx context.Context, item movement.Movement) error {
	_, err := t.q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		BusinessID: uuid.NullUUID{UUID: item.BusinessID, Valid: true}, ActorUserID: nullUUID(item.ActorID),
		Action: "inventory.movement.create", EntityType: "stock_movement",
		EntityID: uuid.NullUUID{UUID: item.ID, Valid: true}, Metadata: []byte(`{}`),
	})
	return err
}

// Params also bridges the legacy edit path while it still uses sqlc directly.
func Params(data movement.Data) db.CreateStockMovementParams {
	return db.CreateStockMovementParams{
		BusinessID: data.BusinessID, VariantID: data.VariantID, LocationID: data.LocationID,
		MovementType: data.MovementType, Quantity: data.Quantity, UnitCost: nullDecimal(data.UnitCost),
		ReferenceType: nullString(data.ReferenceType), ReferenceID: nullString(data.ReferenceID), Notes: data.Notes,
		CreatedBy: nullUUID(data.ActorID), IdempotencyKey: nullString(&data.IdempotencyKey),
	}
}
func mapMovement(item db.StockMovement) movement.Movement {
	var actor *uuid.UUID
	if item.CreatedBy.Valid {
		actor = &item.CreatedBy.UUID
	}
	var cost *decimal.Decimal
	if item.UnitCost.Valid {
		cost = &item.UnitCost.Decimal
	}
	return movement.Movement{ID: item.ID, CreatedAt: item.CreatedAt.Time, Data: movement.Data{
		BusinessID: item.BusinessID, VariantID: item.VariantID, LocationID: item.LocationID, ActorID: actor,
		MovementType: item.MovementType, Quantity: item.Quantity, UnitCost: cost,
		ReferenceType: stringPtr(item.ReferenceType), ReferenceID: stringPtr(item.ReferenceID),
		Notes: item.Notes, IdempotencyKey: item.IdempotencyKey.String,
	}}
}
func mapLevel(level db.StockLevel) movement.StockLevel {
	return movement.StockLevel{VariantID: level.VariantID, LocationID: level.LocationID, QuantityOnHand: level.QuantityOnHand, QuantityReserved: level.QuantityReserved, UpdatedAt: level.UpdatedAt.Time}
}
func nullUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}
func nullDecimal(value *decimal.Decimal) decimal.NullDecimal {
	if value == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *value, Valid: true}
}
func nullString(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
