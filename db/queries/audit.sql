-- name: CreateAuditLog :one
INSERT INTO audit_logs (business_id, actor_user_id, action, entity_type, entity_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

