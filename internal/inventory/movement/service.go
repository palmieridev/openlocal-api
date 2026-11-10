// Package movement owns the create-stock-movement use case and its persistence ports.
// Callers must authorize BusinessID and ActorID before invoking the service.
package movement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrDuplicate = errors.New("stock movement idempotency key exists")
var ErrConflict = errors.New("Idempotency-Key was already used for a different stock movement")
var ErrLocationNotFound = errors.New("inventory location not found")

// Input carries trusted identity separately from untrusted movement fields.
type Input struct {
	BusinessID, ActorID                  uuid.UUID
	VariantID                            string
	LocationID                           *string
	MovementType, Quantity               string
	UnitCost, ReferenceType, ReferenceID *string
	Notes, IdempotencyKey                string
}

type Data struct {
	BusinessID, VariantID, LocationID uuid.UUID
	ActorID                           *uuid.UUID
	MovementType                      string
	Quantity                          decimal.Decimal
	UnitCost                          *decimal.Decimal
	ReferenceType, ReferenceID        *string
	Notes, IdempotencyKey             string
}

type Movement struct {
	Data
	ID        uuid.UUID
	CreatedAt time.Time
}

type StockLevel struct {
	VariantID, LocationID            uuid.UUID
	QuantityOnHand, QuantityReserved decimal.Decimal
	UpdatedAt                        time.Time
}

type Result struct {
	Movement   Movement
	StockLevel StockLevel
	Replayed   bool
}

// Store reads committed state; replay reads must occur after the attempt rolls back.
type Store interface {
	RequireVariant(context.Context, uuid.UUID, uuid.UUID) error
	Begin(context.Context) (Transaction, error)
	FindMovement(context.Context, uuid.UUID, string) (Movement, error)
	StockLevel(context.Context, Data) (StockLevel, error)
}

// Transaction contains only the writes needed for this use case. Insert reports
// ErrDuplicate when the business-scoped idempotency key already exists.
type Transaction interface {
	RequireLocation(context.Context, uuid.UUID, uuid.UUID) error
	DefaultLocation(context.Context, uuid.UUID) (uuid.UUID, error)
	Insert(context.Context, Data) (Movement, error)
	ApplyDelta(context.Context, Data, decimal.Decimal) (StockLevel, error)
	AuditCreated(context.Context, Movement) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) Create(ctx context.Context, input Input) (Result, error) {
	data, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	if err := s.store.RequireVariant(ctx, data.BusinessID, data.VariantID); err != nil {
		return Result{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.LocationID != nil && *input.LocationID != "" {
		data.LocationID, err = parseUUID(*input.LocationID, "location_id")
		if err == nil {
			err = tx.RequireLocation(ctx, data.BusinessID, data.LocationID)
		}
	} else {
		data.LocationID, err = tx.DefaultLocation(ctx, data.BusinessID)
	}
	if err != nil {
		return Result{}, err
	}
	item, err := tx.Insert(ctx, data)
	if errors.Is(err, ErrDuplicate) {
		if err := tx.Rollback(ctx); err != nil {
			return Result{}, err
		}
		return s.replay(ctx, data)
	}
	if err != nil {
		return Result{}, err
	}
	level, err := tx.ApplyDelta(ctx, data, SignedQuantity(data.MovementType, data.Quantity))
	if err != nil {
		return Result{}, err
	}
	if err := tx.AuditCreated(ctx, item); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Movement: item, StockLevel: level}, nil
}

func (s *Service) replay(ctx context.Context, data Data) (Result, error) {
	item, err := s.store.FindMovement(ctx, data.BusinessID, data.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}
	if !Same(item.Data, data) {
		return Result{}, ErrConflict
	}
	level, err := s.store.StockLevel(ctx, item.Data)
	if err != nil {
		return Result{}, err
	}
	return Result{Movement: item, StockLevel: level, Replayed: true}, nil
}

func Same(a, b Data) bool {
	return a.BusinessID == b.BusinessID && a.VariantID == b.VariantID &&
		a.LocationID == b.LocationID && a.MovementType == b.MovementType &&
		a.Quantity.Equal(b.Quantity) && sameDecimal(a.UnitCost, b.UnitCost) &&
		samePointer(a.ReferenceType, b.ReferenceType) && samePointer(a.ReferenceID, b.ReferenceID) &&
		a.Notes == b.Notes && samePointer(a.ActorID, b.ActorID)
}
func samePointer[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func sameDecimal(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
