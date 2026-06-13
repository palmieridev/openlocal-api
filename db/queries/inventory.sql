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
    reference_type, reference_id, notes, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: ApplyStockDelta :one
INSERT INTO stock_levels (business_id, variant_id, location_id, quantity_on_hand)
VALUES ($1, $2, $3, $4)
ON CONFLICT (business_id, variant_id, location_id) DO UPDATE SET
    quantity_on_hand = stock_levels.quantity_on_hand + EXCLUDED.quantity_on_hand,
    updated_at = now()
RETURNING *;

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

