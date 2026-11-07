-- name: AdminSearchBusinesses :many
SELECT id, name, slug, clerk_org_id, business_type, status, timezone, created_at, updated_at
FROM businesses
WHERE (
    sqlc.arg(search_query)::text = ''
    OR strpos(lower(name), lower(sqlc.arg(search_query)::text)) > 0
    OR strpos(lower(slug), lower(sqlc.arg(search_query)::text)) > 0
    OR id::text = sqlc.arg(search_query)::text
    OR clerk_org_id = sqlc.arg(search_query)::text
)
AND (sqlc.narg(before_at)::timestamptz IS NULL
     OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.arg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: AdminGetBusiness :one
SELECT id, name, slug, clerk_org_id, business_type, status, timezone, created_at, updated_at
FROM businesses WHERE id = $1;

-- name: AdminListActivity :many
SELECT id, business_id, actor_user_id, action, entity_type, entity_id, metadata, created_at
FROM audit_logs
WHERE (sqlc.narg(business_id)::uuid IS NULL OR business_id = sqlc.narg(business_id)::uuid)
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR actor_user_id = sqlc.narg(actor_user_id)::uuid)
  AND (sqlc.arg(action_filter)::text = '' OR action = sqlc.arg(action_filter)::text)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.arg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count)::int;
