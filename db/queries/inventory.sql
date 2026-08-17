-- name: CreateInventoryLocation :one
INSERT INTO inventory_locations (business_id, name, is_default)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDefaultInventoryLocation :one
SELECT *
FROM inventory_locations
WHERE business_id = $1 AND is_default = true;

-- name: CreateStockMovement :one
INSERT INTO stock_movements (
    business_id, variant_id, location_id, movement_type, quantity, unit_cost,
    reference_type, reference_id, notes, created_by, idempotency_key
) VALUES (
$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (business_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetStockMovementByIdempotencyKey :one
SELECT *
FROM stock_movements
WHERE business_id = $1 AND idempotency_key = $2;

-- name: GetStockMovementForUpdate :one
SELECT *
FROM stock_movements
WHERE id = $1 AND business_id = $2
FOR UPDATE;

-- name: UpdateStockMovement :one
UPDATE stock_movements
SET location_id = $3,
    movement_type = $4,
    quantity = $5,
    unit_cost = $6,
    reference_type = $7,
    reference_id = $8,
    notes = $9,
    idempotency_key = $10
WHERE id = $1 AND business_id = $2
RETURNING *;

-- name: DeleteStockMovement :one
DELETE FROM stock_movements
WHERE id = $1 AND business_id = $2
RETURNING *;

-- name: ApplyStockDelta :one
INSERT INTO stock_levels (business_id, variant_id, location_id, quantity_on_hand)
VALUES ($1, $2, $3, $4)
ON CONFLICT (business_id, variant_id, location_id) DO UPDATE SET
    quantity_on_hand = stock_levels.quantity_on_hand + EXCLUDED.quantity_on_hand,
    updated_at = now()
RETURNING *;

-- name: ApplyNonnegativeStockDelta :one
UPDATE stock_levels
SET quantity_on_hand = quantity_on_hand + sqlc.arg(quantity_on_hand)::numeric,
    updated_at = now()
WHERE business_id = sqlc.arg(business_id)
  AND variant_id = sqlc.arg(variant_id)
  AND location_id = sqlc.arg(location_id)
  AND quantity_on_hand + sqlc.arg(quantity_on_hand)::numeric >= 0
RETURNING *;

-- name: GetStockLevel :one
SELECT *
FROM stock_levels
WHERE business_id = $1 AND variant_id = $2 AND location_id = $3;

-- name: GetInventoryLocationForBusiness :one
SELECT *
FROM inventory_locations
WHERE id = $1 AND business_id = $2;

-- name: ListStockMovements :many
SELECT *
FROM stock_movements
WHERE business_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListStockLevels :many
SELECT sl.*
FROM stock_levels sl
WHERE sl.business_id = $1
ORDER BY sl.updated_at DESC
LIMIT $2 OFFSET $3;
