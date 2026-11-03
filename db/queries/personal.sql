-- name: SaveBusiness :one
INSERT INTO saved_businesses(user_id,business_id)
SELECT sqlc.arg(user_id),id FROM businesses WHERE id=sqlc.arg(business_id) AND status='active'
ON CONFLICT(user_id,business_id) DO UPDATE SET created_at=saved_businesses.created_at
RETURNING created_at;

-- name: RemoveSavedBusiness :exec
DELETE FROM saved_businesses WHERE user_id=$1 AND business_id=$2;

-- name: IsBusinessSaved :one
SELECT EXISTS(SELECT 1 FROM saved_businesses WHERE user_id=$1 AND business_id=$2)::boolean AS saved;

-- name: ListSavedBusinesses :many
SELECT s.business_id,s.created_at,b.status,b.name,b.slug,b.logo_url
FROM saved_businesses s JOIN businesses b ON b.id=s.business_id
WHERE s.user_id=sqlc.arg(user_id)
AND (sqlc.narg(before_at)::timestamptz IS NULL OR (s.created_at,s.business_id)<(sqlc.narg(before_at)::timestamptz,sqlc.arg(before_id)::uuid))
ORDER BY s.created_at DESC,s.business_id DESC LIMIT sqlc.arg(page_limit);

-- name: PersonalRequest :one
SELECT id,business_id,'appointments'::text AS module FROM appointments
WHERE appointments.customer_user_id=sqlc.arg(user_id) AND appointments.id=sqlc.arg(id) AND sqlc.arg(module)::text='appointments'
UNION ALL
SELECT id,business_id,'preorders'::text AS module FROM preorders
WHERE preorders.customer_user_id=sqlc.arg(user_id) AND preorders.id=sqlc.arg(id) AND sqlc.arg(module)::text='preorders';

-- name: PersonalActivity :many
SELECT r.id,r.business_id,r.module,r.status,r.created_at,r.starts_at,r.ends_at,r.expires_at,r.timezone,
       b.name AS business_name,b.slug AS business_slug,b.status AS business_status,b.logo_url
FROM (
    SELECT id,business_id,'appointments'::text AS module,status,created_at,starts_at,ends_at,expires_at,timezone
    FROM appointments WHERE appointments.customer_user_id=sqlc.arg(user_id)
    AND (sqlc.arg(module)::text='' OR sqlc.arg(module)::text='appointments')
    UNION ALL
    SELECT id,business_id,'preorders'::text AS module,status,created_at,starts_at,ends_at,expires_at,timezone
    FROM preorders WHERE preorders.customer_user_id=sqlc.arg(user_id)
    AND (sqlc.arg(module)::text='' OR sqlc.arg(module)::text='preorders')
) r JOIN businesses b ON b.id=r.business_id
WHERE (sqlc.narg(before_at)::timestamptz IS NULL OR (r.created_at,r.id,r.module)<(sqlc.narg(before_at)::timestamptz,sqlc.arg(before_id)::uuid,sqlc.arg(before_module)::text))
ORDER BY r.created_at DESC,r.id DESC,r.module DESC LIMIT sqlc.arg(page_limit);
