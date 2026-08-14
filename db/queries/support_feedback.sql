-- name: CreateSupportFeedback :one
INSERT INTO support_feedback (doc_id, locale, verdict, comment, path)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;
