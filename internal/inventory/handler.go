package inventory

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/inventory/movements", h.createStockMovement)
	private.Get("/inventory/movements", h.listStockMovements)
	private.Patch("/inventory/movements/:id", h.updateStockMovement)
	private.Delete("/inventory/movements/:id", h.deleteStockMovement)
	private.Get("/inventory/stock-levels", h.listStockLevels)
}

func (h Handler) createStockMovement(c *fiber.Ctx) error {
	var req StockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	idempotencyKey, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	params, businessID, err := MovementParams(req, user.ID, idempotencyKey)
	if err != nil {
		return err
	}
	if _, err := h.rt.Q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: params.VariantID, BusinessID: businessID}); err != nil {
		return err
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	locationID, err := ResolveLocation(c.Context(), qtx, businessID, req.LocationID)
	if err != nil {
		return err
	}
	params.LocationID = locationID
	movement, err := qtx.CreateStockMovement(c.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		if rollbackErr := tx.Rollback(c.Context()); rollbackErr != nil {
			return rollbackErr
		}
		return h.replayStockMovement(c, params)
	}
	if err != nil {
		return err
	}
	level, err := qtx.ApplyStockDelta(c.Context(), db.ApplyStockDeltaParams{
		BusinessID:     businessID,
		VariantID:      params.VariantID,
		LocationID:     locationID,
		QuantityOnHand: SignedQuantity(params.MovementType, params.Quantity),
	})
	if err != nil {
		return err
	}
	if err := api.Audit(qtx, c.Context(), businessID, user.ID, "inventory.movement.create", "stock_movement", movement.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(MovementCreatedResponse{
		Movement:   MapStockMovement(movement),
		StockLevel: MapStockLevel(level),
	})
}

func (h Handler) replayStockMovement(c *fiber.Ctx, params db.CreateStockMovementParams) error {
	movement, err := h.rt.Q.GetStockMovementByIdempotencyKey(c.Context(), db.GetStockMovementByIdempotencyKeyParams{
		BusinessID:     params.BusinessID,
		IdempotencyKey: params.IdempotencyKey,
	})
	if err != nil {
		return err
	}
	if !SameMovement(movement, params) {
		return fiber.NewError(fiber.StatusConflict, "Idempotency-Key was already used for a different stock movement")
	}
	level, err := h.rt.Q.GetStockLevel(c.Context(), db.GetStockLevelParams{
		BusinessID: movement.BusinessID,
		VariantID:  movement.VariantID,
		LocationID: movement.LocationID,
	})
	if err != nil {
		return err
	}
	c.Set("Idempotent-Replayed", "true")
	return c.JSON(MovementCreatedResponse{
		Movement:   MapStockMovement(movement),
		StockLevel: MapStockLevel(level),
	})
}

func (h Handler) updateStockMovement(c *fiber.Ctx) error {
	movementID, err := v.ParseUUID(c.Params("id"), "movement id")
	if err != nil {
		return err
	}
	var req StockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	idempotencyKey, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	current, err := qtx.GetStockMovementForUpdate(c.Context(), db.GetStockMovementForUpdateParams{
		ID: movementID, BusinessID: businessID,
	})
	if err != nil {
		return err
	}
	parsed, parsedBusinessID, err := MovementParams(req, user.ID, idempotencyKey)
	if err != nil {
		return err
	}
	if parsedBusinessID != current.BusinessID {
		return fiber.NewError(fiber.StatusNotFound, "not found")
	}
	if parsed.VariantID != current.VariantID {
		return fiber.NewError(fiber.StatusBadRequest, "variant_id cannot be changed; delete and recreate the movement instead")
	}
	locationID := current.LocationID
	if req.LocationID != nil && *req.LocationID != "" {
		locationID, err = v.ParseUUID(*req.LocationID, "location_id")
		if err != nil {
			return err
		}
		if _, err := qtx.GetInventoryLocationForBusiness(c.Context(), db.GetInventoryLocationForBusinessParams{
			ID: locationID, BusinessID: businessID,
		}); err != nil {
			return err
		}
	}
	params := db.UpdateStockMovementParams{
		ID:             movementID,
		BusinessID:     businessID,
		LocationID:     locationID,
		MovementType:   parsed.MovementType,
		Quantity:       parsed.Quantity,
		UnitCost:       parsed.UnitCost,
		ReferenceType:  parsed.ReferenceType,
		ReferenceID:    parsed.ReferenceID,
		Notes:          parsed.Notes,
		IdempotencyKey: parsed.IdempotencyKey,
	}
	if current.IdempotencyKey == params.IdempotencyKey {
		if !SameMovementEdit(current, params) {
			return fiber.NewError(fiber.StatusConflict, "Idempotency-Key was already used for different movement data")
		}
		level, err := qtx.GetStockLevel(c.Context(), db.GetStockLevelParams{
			BusinessID: businessID, VariantID: current.VariantID, LocationID: current.LocationID,
		})
		if err != nil {
			return err
		}
		if err := tx.Commit(c.Context()); err != nil {
			return err
		}
		c.Set("Idempotent-Replayed", "true")
		return c.JSON(MovementCreatedResponse{Movement: MapStockMovement(current), StockLevel: MapStockLevel(level)})
	}
	level, err := applyMovementEditDelta(c.Context(), qtx, current, params)
	if err != nil {
		return err
	}
	movement, err := qtx.UpdateStockMovement(c.Context(), params)
	if isUniqueViolation(err) {
		return fiber.NewError(fiber.StatusConflict, "Idempotency-Key was already used for a different stock movement")
	}
	if err != nil {
		return err
	}
	if err := api.Audit(qtx, c.Context(), businessID, user.ID, "inventory.movement.update", "stock_movement", movement.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(MovementCreatedResponse{Movement: MapStockMovement(movement), StockLevel: MapStockLevel(level)})
}

func (h Handler) deleteStockMovement(c *fiber.Ctx) error {
	movementID, businessID, err := api.IDAndBusinessFromRequest(c, "movement id")
	if err != nil {
		return err
	}
	_, user, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	tx, err := h.rt.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer api.Rollback(c.Context(), tx)
	qtx := h.rt.Q.WithTx(tx)
	movement, err := qtx.GetStockMovementForUpdate(c.Context(), db.GetStockMovementForUpdateParams{
		ID: movementID, BusinessID: businessID,
	})
	if err != nil {
		return err
	}
	if _, err := applyNonnegativeStockDelta(c.Context(), qtx, movement.BusinessID, movement.VariantID, movement.LocationID,
		SignedQuantity(movement.MovementType, movement.Quantity).Neg()); err != nil {
		return err
	}
	if _, err := qtx.DeleteStockMovement(c.Context(), db.DeleteStockMovementParams{ID: movementID, BusinessID: businessID}); err != nil {
		return err
	}
	if err := api.Audit(qtx, c.Context(), businessID, user.ID, "inventory.movement.delete", "stock_movement", movement.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func applyMovementEditDelta(ctx context.Context, q *db.Queries, current db.StockMovement, params db.UpdateStockMovementParams) (db.StockLevel, error) {
	if current.LocationID == params.LocationID {
		return applyNonnegativeStockDelta(ctx, q, current.BusinessID, current.VariantID, current.LocationID,
			MovementDelta(current, params.MovementType, params.Quantity))
	}
	if _, err := applyNonnegativeStockDelta(ctx, q, current.BusinessID, current.VariantID, current.LocationID,
		SignedQuantity(current.MovementType, current.Quantity).Neg()); err != nil {
		return db.StockLevel{}, err
	}
	return applyNonnegativeStockDelta(ctx, q, current.BusinessID, current.VariantID, params.LocationID,
		SignedQuantity(params.MovementType, params.Quantity))
}

func applyNonnegativeStockDelta(ctx context.Context, q *db.Queries, businessID, variantID, locationID uuid.UUID, delta decimal.Decimal) (db.StockLevel, error) {
	if delta.IsZero() {
		return q.GetStockLevel(ctx, db.GetStockLevelParams{
			BusinessID: businessID, VariantID: variantID, LocationID: locationID,
		})
	}
	level, err := q.ApplyNonnegativeStockDelta(ctx, db.ApplyNonnegativeStockDeltaParams{
		BusinessID: businessID, VariantID: variantID, LocationID: locationID, QuantityOnHand: delta,
	})
	if errors.Is(err, pgx.ErrNoRows) && delta.IsPositive() {
		_, lookupErr := q.GetStockLevel(ctx, db.GetStockLevelParams{
			BusinessID: businessID, VariantID: variantID, LocationID: locationID,
		})
		if lookupErr == nil {
			return db.StockLevel{}, fiber.NewError(fiber.StatusConflict, "stock level cannot be negative")
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return db.StockLevel{}, lookupErr
		}
		return q.ApplyStockDelta(ctx, db.ApplyStockDeltaParams{
			BusinessID: businessID, VariantID: variantID, LocationID: locationID, QuantityOnHand: delta,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.StockLevel{}, fiber.NewError(fiber.StatusConflict, "stock level cannot be negative")
	}
	return level, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (h Handler) listStockMovements(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.ListStockMovements(c.Context(), db.ListStockMovementsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	out := make([]StockMovementResponse, 0, len(items))
	for _, item := range items {
		out = append(out, MapStockMovement(item))
	}
	return c.JSON(out)
}

func (h Handler) listStockLevels(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.ListStockLevels(c.Context(), db.ListStockLevelsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	out := make([]StockLevelResponse, 0, len(items))
	for _, item := range items {
		out = append(out, MapStockLevel(item))
	}
	return c.JSON(out)
}
