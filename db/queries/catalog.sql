-- name: CreateProduct :one
INSERT INTO products (business_id, category_id, name, slug, description, brand, unit, product_type, is_handmade, is_public, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListProducts :many
SELECT p.*,
       COALESCE(pi.url, '') AS image_url
FROM products p
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE product_id = p.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE p.business_id = $1
ORDER BY p.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetProductForBusiness :one
SELECT *
FROM products
WHERE id = $1 AND business_id = $2;

-- name: UpdateProduct :one
UPDATE products SET
    category_id = $3,
    name = $4,
    description = $5,
    brand = $6,
    unit = $7,
    product_type = $8,
    is_handmade = $9,
    is_public = $10,
    status = $11,
    updated_at = now()
WHERE id = $1 AND business_id = $2
RETURNING *;

-- name: ArchiveProduct :exec
UPDATE products SET status = 'archived', updated_at = now()
WHERE id = $1 AND business_id = $2;

-- name: CreateVariant :one
INSERT INTO product_variants (
    product_id, business_id, sku, barcode, internal_code, name, attributes,
    price, cost, currency, track_inventory, public_stock_status, reorder_point,
    lead_time_days, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING *;

-- name: ListVariantsByProduct :many
SELECT *
FROM product_variants
WHERE product_id = $1 AND business_id = $2
ORDER BY created_at ASC;

-- name: GetVariantForBusiness :one
SELECT *
FROM product_variants
WHERE id = $1 AND business_id = $2;

-- name: GetVariantByBarcode :one
SELECT *
FROM product_variants
WHERE business_id = $1 AND barcode = $2 AND status = 'active';

-- name: GetVariantBySKU :one
SELECT *
FROM product_variants
WHERE business_id = $1 AND sku = $2 AND status = 'active';

-- name: UpdateVariant :one
UPDATE product_variants SET
    sku = $3,
    barcode = $4,
    internal_code = $5,
    name = $6,
    attributes = $7,
    price = $8,
    cost = $9,
    currency = $10,
    track_inventory = $11,
    public_stock_status = $12,
    reorder_point = $13,
    lead_time_days = $14,
    status = $15,
    updated_at = now()
WHERE id = $1 AND business_id = $2
RETURNING *;

-- name: ArchiveVariant :exec
UPDATE product_variants SET status = 'archived', updated_at = now()
WHERE id = $1 AND business_id = $2;

-- name: ListPublicProductsByBusinessSlug :many
SELECT p.id, p.name, p.slug, p.description, p.brand, p.unit, p.product_type,
       pv.id AS variant_id, pv.sku, pv.name AS variant_name, pv.price, pv.currency,
       pv.public_stock_status,
       COALESCE(pi.url, '') AS image_url
FROM businesses b
JOIN products p ON p.business_id = b.id
JOIN product_variants pv ON pv.product_id = p.id
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE product_id = p.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE b.slug = $1
  AND b.status = 'active'
  AND p.is_public = true
  AND p.status = 'active'
  AND pv.status = 'active'
ORDER BY p.name ASC, pv.created_at ASC
LIMIT $2 OFFSET $3;

-- name: SearchMarketplaceProducts :many
SELECT b.slug AS business_slug, b.name AS business_name,
       p.id, p.name, p.slug, p.description, p.brand, p.unit, p.product_type,
       pv.id AS variant_id, pv.sku, pv.name AS variant_name, pv.price, pv.currency,
       pv.public_stock_status,
       COALESCE(pi.url, '') AS image_url
FROM businesses b
JOIN products p ON p.business_id = b.id
JOIN product_variants pv ON pv.product_id = p.id
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE product_id = p.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE b.status = 'active'
  AND p.is_public = true
  AND p.status = 'active'
  AND pv.status = 'active'
  AND ($1::text = '' OR to_tsvector('simple', p.name || ' ' || p.description) @@ plainto_tsquery('simple', $1))
ORDER BY p.name ASC
LIMIT $2 OFFSET $3;
