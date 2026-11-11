package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// Write uses the supplied queries so callers retain ownership of the transaction.
func Write(q *db.Queries, ctx context.Context, businessID, userID uuid.UUID, action, entityType string, entityID uuid.UUID) error {
	_, err := q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		BusinessID:  uuid.NullUUID{UUID: businessID, Valid: true},
		ActorUserID: uuid.NullUUID{UUID: userID, Valid: true},
		Action:      action,
		EntityType:  entityType,
		EntityID:    uuid.NullUUID{UUID: entityID, Valid: true},
		Metadata:    []byte(`{}`),
	})
	return err
}

// WithMetadata writes through the supplied queries, including its transaction.
// A zero UUID represents an absent business, actor, or entity. Metadata must be
// JSON serializable; nil is stored as an empty object.
func WithMetadata(q *db.Queries, ctx context.Context, businessID, userID uuid.UUID, action, entityType string, entityID uuid.UUID, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		BusinessID:  uuid.NullUUID{UUID: businessID, Valid: businessID != uuid.Nil},
		ActorUserID: uuid.NullUUID{UUID: userID, Valid: userID != uuid.Nil},
		Action:      action, EntityType: entityType,
		EntityID: uuid.NullUUID{UUID: entityID, Valid: entityID != uuid.Nil},
		Metadata: data,
	})
	return err
}
