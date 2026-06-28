-- name: UpsertUserFromClerk :one
INSERT INTO users (clerk_user_id, email, first_name, last_name, image_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (clerk_user_id) DO UPDATE SET
    email = EXCLUDED.email,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    image_url = EXCLUDED.image_url,
    updated_at = now()
RETURNING *;

-- name: GetUserByClerkID :one
SELECT * FROM users WHERE clerk_user_id = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: DeleteUserByClerkID :exec
DELETE FROM users
WHERE clerk_user_id = $1;
