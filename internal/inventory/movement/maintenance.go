package movement

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrNegativeStock = errors.New("stock level cannot be negative")
	ErrEditConflict  = errors.New("Idempotency-Key was already used for different movement data")
)

type EditInput struct {
	Input
	MovementID uuid.UUID
}

// DeleteInput carries identity already authorized by the caller boundary.
type DeleteInput struct{ BusinessID, ActorID, MovementID uuid.UUID }

type MaintenanceStore interface {
	BeginChange(context.Context) (ChangeTransaction, error)
}

// ChangeTransaction must lock the business-scoped movement until commit or
// rollback. Stock deltas must check the resulting quantity atomically. All
// movement, stock and audit writes share this transaction. LockStockLocations
// must preserve a consistent transfer order even when a stock row is absent
// and another transaction creates it before the deltas are applied.
type ChangeTransaction interface {
	LockMovement(context.Context, uuid.UUID, uuid.UUID) (Movement, error)
	RequireVariant(context.Context, uuid.UUID, uuid.UUID) error
	RequireLocation(context.Context, uuid.UUID, uuid.UUID) error
	StockLevel(context.Context, Data) (StockLevel, error)
	LockStockLocations(context.Context, Data, []uuid.UUID) error
	ApplyNonnegativeDelta(context.Context, Data, decimal.Decimal) (StockLevel, error)
	Update(context.Context, Movement) (Movement, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	AuditChange(context.Context, DeleteInput, string) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Maintenance struct{ store MaintenanceStore }

func NewMaintenance(store MaintenanceStore) *Maintenance { return &Maintenance{store: store} }

func (s *Maintenance) Edit(ctx context.Context, input EditInput) (Result, error) {
	tx, err := s.store.BeginChange(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := tx.LockMovement(ctx, input.BusinessID, input.MovementID)
	if err != nil {
		return Result{}, err
	}
	data, err := Parse(input.Input)
	if err != nil {
		return Result{}, err
	}
	if data.VariantID != current.VariantID {
		return Result{}, invalid("variant_id cannot be changed; delete and recreate the movement instead")
	}
	if err := requireAssociations(ctx, tx, current.Data); err != nil {
		return Result{}, err
	}
	data.LocationID = current.LocationID
	if input.LocationID != nil && *input.LocationID != "" {
		data.LocationID, err = parseUUID(*input.LocationID, "location_id")
		if err != nil {
			return Result{}, err
		}
		if err := tx.RequireLocation(ctx, data.BusinessID, data.LocationID); err != nil {
			return Result{}, err
		}
	}
	// Edits preserve the original creator and creation timestamp. The authenticated
	// editor is used only for the audit; edit replay deliberately ignores actor.
	data.ActorID = current.ActorID
	if current.IdempotencyKey == data.IdempotencyKey {
		if !Same(current.Data, data) {
			return Result{}, ErrEditConflict
		}
		level, err := tx.StockLevel(ctx, current.Data)
		if err != nil {
			return Result{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
		return Result{Movement: current, StockLevel: level, Replayed: true}, nil
	}
	var level StockLevel
	if current.LocationID == data.LocationID {
		level, err = tx.ApplyNonnegativeDelta(ctx, data, SignedQuantity(data.MovementType, data.Quantity).Sub(SignedQuantity(current.MovementType, current.Quantity)))
	} else {
		if err := tx.LockStockLocations(ctx, data, []uuid.UUID{current.LocationID, data.LocationID}); err != nil {
			return Result{}, err
		}
		_, err = tx.ApplyNonnegativeDelta(ctx, current.Data, SignedQuantity(current.MovementType, current.Quantity).Neg())
		if err == nil {
			level, err = tx.ApplyNonnegativeDelta(ctx, data, SignedQuantity(data.MovementType, data.Quantity))
		}
	}
	if err != nil {
		return Result{}, err
	}
	updated, err := tx.Update(ctx, Movement{ID: current.ID, CreatedAt: current.CreatedAt, Data: data})
	if err != nil {
		return Result{}, err
	}
	if err := tx.AuditChange(ctx, DeleteInput{BusinessID: input.BusinessID, ActorID: input.ActorID, MovementID: current.ID}, "inventory.movement.update"); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{Movement: updated, StockLevel: level}, nil
}

func (s *Maintenance) Delete(ctx context.Context, input DeleteInput) error {
	tx, err := s.store.BeginChange(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := tx.LockMovement(ctx, input.BusinessID, input.MovementID)
	if err != nil {
		return err
	}
	if err := requireAssociations(ctx, tx, current.Data); err != nil {
		return err
	}
	if _, err := tx.ApplyNonnegativeDelta(ctx, current.Data, SignedQuantity(current.MovementType, current.Quantity).Neg()); err != nil {
		return err
	}
	if err := tx.Delete(ctx, input.BusinessID, input.MovementID); err != nil {
		return err
	}
	if err := tx.AuditChange(ctx, input, "inventory.movement.delete"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func requireAssociations(ctx context.Context, tx ChangeTransaction, data Data) error {
	if err := tx.RequireVariant(ctx, data.BusinessID, data.VariantID); err != nil {
		return err
	}
	return tx.RequireLocation(ctx, data.BusinessID, data.LocationID)
}
