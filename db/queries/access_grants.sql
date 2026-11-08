-- name: CreateAccessGrant :one
INSERT INTO business_access_grants(business_id,module,tier,starts_at,expires_at,internal_reason,owner_message,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: GetAccessGrant :one
SELECT * FROM business_access_grants WHERE business_id=$1 AND id=$2;

-- name: ListAccessGrants :many
SELECT * FROM business_access_grants WHERE business_id=$1 ORDER BY created_at DESC,id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAccessGrants :one
SELECT count(*)::int FROM business_access_grants WHERE business_id=$1;

-- name: AccessGrantsForResolution :many
SELECT * FROM business_access_grants WHERE business_id=$1 AND starts_at <= sqlc.arg(at)::timestamptz
ORDER BY created_at,id;

-- name: ExtendAccessGrant :one
UPDATE business_access_grants SET expires_at=$3,version=version+1,updated_at=clock_timestamp()
WHERE business_id=$1 AND id=$2 AND version=$4 AND revoked_at IS NULL RETURNING *;

-- name: RevokeAccessGrant :one
UPDATE business_access_grants SET revoked_at=clock_timestamp(),revoked_by=$3,revocation_reason=$4,
version=version+1,updated_at=clock_timestamp()
WHERE business_id=$1 AND id=$2 AND version=$5 AND revoked_at IS NULL RETURNING *;

-- name: AccessGrantHistory :many
SELECT id,actor_user_id,action,metadata,created_at FROM audit_logs
WHERE business_id=$1 AND entity_type='access_grant' AND entity_id=$2
ORDER BY (metadata->'after'->>'version')::integer,id LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: ModuleUsageInWindow :one
SELECT count(*)::int FROM module_usage_events WHERE business_id=$1 AND module=$2
AND created_at >= sqlc.arg(window_start)::timestamptz AND created_at < sqlc.arg(window_end)::timestamptz;
