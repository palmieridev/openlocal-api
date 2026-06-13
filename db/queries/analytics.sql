-- name: GetABCAnalysis :many
WITH product_values AS (
    SELECT p.id AS product_id, p.name, COALESCE(SUM(pss.sales_revenue), 0)::numeric AS value
    FROM products p
    LEFT JOIN product_sales_summary pss ON pss.product_id = p.id
    WHERE p.business_id = $1
    GROUP BY p.id, p.name
),
ranked AS (
    SELECT product_id, name, value,
           COALESCE(SUM(value) OVER (), 0)::numeric AS total_value,
           COALESCE(SUM(value) OVER (ORDER BY value DESC, name ASC), 0)::numeric AS cumulative_value
    FROM product_values
)
SELECT product_id, name, value, total_value, cumulative_value,
       CASE
           WHEN total_value = 0 THEN 'C'
           WHEN cumulative_value / total_value <= 0.80 THEN 'A'
           WHEN cumulative_value / total_value <= 0.95 THEN 'B'
           ELSE 'C'
       END AS class
FROM ranked
ORDER BY value DESC, name ASC
LIMIT $2 OFFSET $3;

-- name: GetLowStock :many
SELECT pv.id AS variant_id, pv.product_id, pv.sku, pv.name, sl.quantity_on_hand, pv.reorder_point
FROM product_variants pv
JOIN stock_levels sl ON sl.variant_id = pv.id
WHERE pv.business_id = $1
  AND pv.track_inventory = true
  AND sl.quantity_on_hand <= pv.reorder_point
ORDER BY sl.quantity_on_hand ASC
LIMIT $2 OFFSET $3;

-- name: GetDemandForEOQ :one
SELECT COALESCE(SUM(quantity), 0)::numeric AS demand
FROM stock_movements
WHERE business_id = $1
  AND variant_id = $2
  AND movement_type = 'OUT_SALE'
  AND created_at >= now() - ($3::int * interval '1 day');

