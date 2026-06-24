-- name: CreateBusiness :one
INSERT INTO businesses (
    name, slug, description, business_type, phone, whatsapp, email, website,
    logo_url, cover_image_url, status, address, neighborhood, city, state,
    country, postal_code, latitude, longitude, pickup_available, delivery_available
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
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
    updated_at = now()
WHERE businesses.id = $1 AND EXISTS (
    SELECT 1 FROM business_members bm
    WHERE bm.business_id = businesses.id
      AND bm.user_id = $2
      AND bm.role IN ('owner', 'manager')
)
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
SELECT business_id
FROM business_members
WHERE clerk_org_id = $1
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
WHERE id IN (
    SELECT business_id
    FROM business_members
    WHERE clerk_org_id = $1
);

-- name: ListPublicBusinesses :many
SELECT id, name, slug, description, business_type, logo_url, cover_image_url,
       city, state, country, latitude, longitude, pickup_available, delivery_available
FROM businesses
WHERE status = 'active'
  AND (sqlc.narg('city')::text IS NULL OR city = sqlc.narg('city')::text)
  AND (sqlc.narg('min_lat')::numeric IS NULL OR latitude >= sqlc.narg('min_lat')::numeric)
  AND (sqlc.narg('max_lat')::numeric IS NULL OR latitude <= sqlc.narg('max_lat')::numeric)
  AND (sqlc.narg('min_lng')::numeric IS NULL OR longitude >= sqlc.narg('min_lng')::numeric)
  AND (sqlc.narg('max_lng')::numeric IS NULL OR longitude <= sqlc.narg('max_lng')::numeric)
ORDER BY name ASC
LIMIT sqlc.arg('limit_count') OFFSET sqlc.arg('offset_count');

-- name: GetPublicBusinessBySlug :one
SELECT id, name, slug, description, business_type, phone, whatsapp, email, website,
       logo_url, cover_image_url, address, neighborhood, city, state, country,
       postal_code, latitude, longitude, pickup_available, delivery_available
FROM businesses
WHERE slug = $1 AND status = 'active';
