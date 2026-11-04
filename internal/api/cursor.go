package api

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Cursor is a position, never an authorization credential. Queries must always
// independently constrain results to the authenticated user.
type Cursor struct {
	Scope  string    `json:"scope"`
	At     time.Time `json:"at"`
	ID     uuid.UUID `json:"id"`
	Module string    `json:"module,omitempty"`
}

type CursorPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Limit      int32   `json:"limit"`
}

func ParseCursor(c *fiber.Ctx, scope string) (Cursor, int32, error) {
	var cursor Cursor
	n, err := strconv.Atoi(c.Query("limit", "25"))
	if err != nil || n < 1 || n > 100 {
		return cursor, 0, Fail(400, "invalid_request", "limit must be 1 to 100")
	}
	raw := c.Query("cursor")
	if raw == "" {
		return cursor, int32(n), nil
	}
	if len(raw) > 1024 {
		return cursor, 0, Fail(400, "invalid_request", "invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope || cursor.At.IsZero() || cursor.ID == uuid.Nil {
		return Cursor{}, 0, Fail(400, "invalid_request", "invalid cursor")
	}
	return cursor, int32(n), nil
}
func EncodeCursor(scope string, at time.Time, id uuid.UUID, module string) *string {
	data, _ := json.Marshal(Cursor{scope, at, id, module})
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return &encoded
}
