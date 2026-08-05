-- name: CreateBusiness :one
INSERT INTO businesses (
    clerk_org_id, name, slug, description, business_type, phone, whatsapp, email, website,
    logo_url, cover_image_url, status, address, neighborhood, city, state,
    country, postal_code, latitude, longitude, pickup_available, delivery_available, timezone,
    location_mode
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
)
RETURNING *;

-- name: UpdateBusiness :one
UPDATE businesses SET
    name = $3,
    description = $4,
    business_type = $5,
    phone = $6,
    whatsapp = $7,
    email = $8,
    website = $9,
    logo_url = $10,
    cover_image_url = $11,
    status = $12,
    address = $13,
    neighborhood = $14,
    city = $15,
    state = $16,
    country = $17,
    postal_code = $18,
    latitude = $19,
    longitude = $20,
    pickup_available = $21,
    delivery_available = $22,
    timezone = $23,
    location_mode = $24,
    updated_at = now()
WHERE businesses.id = $1 AND EXISTS (
    SELECT 1 FROM business_members bm
    WHERE bm.business_id = businesses.id
      AND bm.user_id = $2
      AND bm.role IN ('owner', 'manager')
)
RETURNING *;

-- name: ListBusinessHours :many
-- day_of_week is 0=Monday .. 6=Sunday.
SELECT *
FROM business_hours
WHERE business_id = $1
ORDER BY day_of_week;

-- name: ListBusinessHoursForBusinesses :many
-- Batch variant so marketplace listings fetch every business's hours in one
-- round trip instead of one query per business.
SELECT *
FROM business_hours
WHERE business_id = ANY(@business_ids::uuid[])
ORDER BY business_id, day_of_week;

-- name: DeleteBusinessHours :exec
DELETE FROM business_hours
WHERE business_id = $1;

-- name: UpsertBusinessHour :one
INSERT INTO business_hours (business_id, day_of_week, opens_at, closes_at, is_closed)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (business_id, day_of_week) DO UPDATE SET
    opens_at = EXCLUDED.opens_at,
    closes_at = EXCLUDED.closes_at,
    is_closed = EXCLUDED.is_closed
RETURNING *;

-- name: GetBusinessForMember :one
SELECT b.*
FROM businesses b
JOIN business_members bm ON bm.business_id = b.id
WHERE b.id = $1 AND bm.user_id = $2;

-- name: GetBusinessMemberRole :one
SELECT role
FROM business_members
WHERE business_id = $1 AND user_id = $2 AND clerk_org_id = $3;

-- name: GetBusinessIDByClerkOrgID :one
SELECT id
FROM businesses
WHERE businesses.clerk_org_id = $1::text
UNION
SELECT business_id
FROM business_members
WHERE business_members.clerk_org_id = $1::text
LIMIT 1;

-- name: AddBusinessMember :one
INSERT INTO business_members (business_id, user_id, clerk_org_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (business_id, user_id) DO UPDATE SET
    clerk_org_id = EXCLUDED.clerk_org_id,
    role = EXCLUDED.role,
    updated_at = now()
RETURNING *;

-- name: DeleteBusinessMemberByClerkOrgAndUser :exec
DELETE FROM business_members
WHERE clerk_org_id = $1 AND user_id = $2;

-- name: ArchiveBusinessesByClerkOrgID :exec
UPDATE businesses
SET status = 'archived',
    updated_at = now()
WHERE businesses.clerk_org_id = $1::text
   OR id IN (
    SELECT business_id
    FROM business_members
    WHERE business_members.clerk_org_id = $1::text
);

-- name: ListPublicBusinesses :many
SELECT id, name, slug, description, business_type, logo_url, cover_image_url,
       city, state, country, latitude, longitude, pickup_available, delivery_available,
       timezone, location_mode
FROM businesses b
WHERE status = 'active'
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
ORDER BY name ASC
LIMIT sqlc.arg('limit_count') OFFSET sqlc.arg('offset_count');

-- name: GetPublicBusinessBySlug :one
SELECT id, name, slug, description, business_type, phone, whatsapp, email, website,
       logo_url, cover_image_url, address, neighborhood, city, state, country,
       postal_code, latitude, longitude, pickup_available, delivery_available, timezone,
       location_mode
FROM businesses
WHERE slug = $1 AND status = 'active';

-- name: CreateBusinessServiceArea :one
INSERT INTO business_service_areas (
    business_id, name, country, state, municipality, city, neighborhood, postal_code,
    country_key, state_key, municipality_key, city_key, neighborhood_key,
    postal_code_key, normalized_key
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING *;

-- name: DeleteBusinessServiceAreas :exec
DELETE FROM business_service_areas
WHERE business_id = $1;

-- name: ListBusinessServiceAreas :many
SELECT *
FROM business_service_areas
WHERE business_id = $1
ORDER BY name, id;

-- name: ListBusinessServiceAreasForBusinesses :many
SELECT *
FROM business_service_areas
WHERE business_id = ANY(@business_ids::uuid[])
ORDER BY business_id, name, id;
