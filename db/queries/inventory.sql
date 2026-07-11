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

-- name: ApplyStockDelta :one
INSERT INTO stock_levels (business_id, variant_id, location_id, quantity_on_hand)
VALUES ($1, $2, $3, $4)
ON CONFLICT (business_id, variant_id, location_id) DO UPDATE SET
    quantity_on_hand = stock_levels.quantity_on_hand + EXCLUDED.quantity_on_hand,
    updated_at = now()
RETURNING *;

-- name: GetStockLevel :one
SELECT *
FROM stock_levels
WHERE business_id = $1 AND variant_id = $2 AND location_id = $3;

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
