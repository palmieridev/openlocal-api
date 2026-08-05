-- name: CreateProduct :one
INSERT INTO products (business_id, category_id, name, slug, description, brand, unit, product_type, is_handmade, is_public, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListProducts :many
SELECT p.*
FROM products p
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

-- Storefront image. Variants carry at most one today, so writes replace the
-- whole set; the scalar subquery keeps the read row-safe (no ErrNoRows).
-- name: GetVariantImage :one
SELECT COALESCE(
    (SELECT url
     FROM product_images
     WHERE variant_id = $1
     ORDER BY position ASC, created_at ASC
     LIMIT 1),
    ''
)::text AS url;

-- name: DeleteVariantImages :exec
DELETE FROM product_images WHERE variant_id = $1;

-- name: CreateVariantImage :exec
INSERT INTO product_images (variant_id, url, position) VALUES ($1, $2, 0);

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
SELECT sqlc.embed(pv),
       COALESCE(pi.url, '') AS image_url
FROM product_variants pv
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE variant_id = pv.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE pv.product_id = $1 AND pv.business_id = $2
ORDER BY pv.created_at ASC;

-- name: GetVariantForBusiness :one
SELECT sqlc.embed(pv),
       COALESCE(pi.url, '') AS image_url
FROM product_variants pv
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE variant_id = pv.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE pv.id = $1 AND pv.business_id = $2;

-- name: GetVariantByBarcode :one
SELECT sqlc.embed(pv),
       COALESCE(pi.url, '') AS image_url
FROM product_variants pv
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE variant_id = pv.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE pv.business_id = $1 AND pv.barcode = $2 AND pv.status = 'active';

-- name: GetVariantBySKU :one
SELECT sqlc.embed(pv),
       COALESCE(pi.url, '') AS image_url
FROM product_variants pv
LEFT JOIN LATERAL (
    SELECT url
    FROM product_images
    WHERE variant_id = pv.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE pv.business_id = $1 AND pv.sku = $2 AND pv.status = 'active';

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
    WHERE variant_id = pv.id
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
    WHERE variant_id = pv.id
    ORDER BY position ASC, created_at ASC
    LIMIT 1
) pi ON true
WHERE b.status = 'active'
  AND p.is_public = true
  AND p.status = 'active'
  AND pv.status = 'active'
  AND (sqlc.arg('search_query')::text = '' OR to_tsvector('simple', p.name || ' ' || p.description) @@ plainto_tsquery('simple', sqlc.arg('search_query')::text))
  AND (
    (NOT sqlc.arg('has_bbox')::boolean AND NOT sqlc.arg('has_area_filter')::boolean)
    OR (
      b.location_mode IN ('fixed', 'hybrid')
      AND (
        (sqlc.arg('has_bbox')::boolean
          AND (sqlc.narg('min_lat')::numeric IS NULL OR b.latitude >= sqlc.narg('min_lat')::numeric)
          AND (sqlc.narg('max_lat')::numeric IS NULL OR b.latitude <= sqlc.narg('max_lat')::numeric)
          AND (sqlc.narg('min_lng')::numeric IS NULL OR b.longitude >= sqlc.narg('min_lng')::numeric)
          AND (sqlc.narg('max_lng')::numeric IS NULL OR b.longitude <= sqlc.narg('max_lng')::numeric)
        )
        OR (sqlc.arg('has_area_filter')::boolean
          AND (sqlc.narg('country')::text IS NULL OR lower(b.country) = lower(sqlc.narg('country')::text))
          AND (sqlc.narg('state')::text IS NULL OR lower(b.state) = lower(sqlc.narg('state')::text))
          AND (sqlc.narg('municipality')::text IS NULL OR lower(b.city) = lower(sqlc.narg('municipality')::text) OR lower(b.neighborhood) = lower(sqlc.narg('municipality')::text))
          AND (sqlc.narg('city')::text IS NULL OR lower(b.city) = lower(sqlc.narg('city')::text))
          AND (sqlc.narg('neighborhood')::text IS NULL OR lower(b.neighborhood) = lower(sqlc.narg('neighborhood')::text))
          AND (sqlc.narg('postal_code')::text IS NULL OR b.postal_code = sqlc.narg('postal_code')::text)
        )
      )
    )
    OR (
      sqlc.arg('has_area_filter')::boolean
      AND b.location_mode IN ('mobile', 'hybrid')
      AND EXISTS (
        SELECT 1
        FROM business_service_areas sa
        WHERE sa.business_id = b.id
          AND (sqlc.narg('country_key')::text IS NULL OR sa.country_key = sqlc.narg('country_key')::text)
          AND (sqlc.narg('state_key')::text IS NULL OR sa.state_key = sqlc.narg('state_key')::text)
          AND (sqlc.narg('municipality_key')::text IS NULL OR sa.municipality_key IS NULL OR sa.municipality_key = sqlc.narg('municipality_key')::text)
          AND (sqlc.narg('city_key')::text IS NULL OR sa.city_key IS NULL OR sa.city_key = sqlc.narg('city_key')::text)
          AND (sqlc.narg('neighborhood_key')::text IS NULL OR sa.neighborhood_key IS NULL OR sa.neighborhood_key = sqlc.narg('neighborhood_key')::text)
          AND (sqlc.narg('postal_code_key')::text IS NULL OR sa.postal_code_key IS NULL OR sa.postal_code_key = sqlc.narg('postal_code_key')::text)
      )
    )
  )
ORDER BY p.name ASC
LIMIT sqlc.arg('limit_count') OFFSET sqlc.arg('offset_count');
