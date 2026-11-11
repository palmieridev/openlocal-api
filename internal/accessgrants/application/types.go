// Package application owns platform administrator grant workflows.
package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CreateInput struct {
	Module         string     `json:"module"`
	Tier           string     `json:"tier"`
	StartsAt       time.Time  `json:"starts_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Indefinite     bool       `json:"indefinite"`
	InternalReason string     `json:"internal_reason"`
	OwnerMessage   *string    `json:"owner_message"`
}
type ExtendInput struct {
	Version        int32      `json:"version"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Indefinite     bool       `json:"indefinite"`
	InternalReason string     `json:"internal_reason"`
}
type RevokeInput struct {
	Version        int32  `json:"version"`
	InternalReason string `json:"internal_reason"`
}

// Grant is a private platform-admin projection, never used in owner entitlements.
type Grant struct {
	ID               uuid.UUID  `json:"id"`
	BusinessID       uuid.UUID  `json:"business_id"`
	Module           string     `json:"module"`
	Tier             string     `json:"tier"`
	StartsAt         time.Time  `json:"starts_at"`
	ExpiresAt        *time.Time `json:"expires_at"`
	Indefinite       bool       `json:"indefinite"`
	Status           string     `json:"status"`
	InternalReason   string     `json:"internal_reason"`
	OwnerMessage     *string    `json:"owner_message"`
	CreatedBy        *uuid.UUID `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	Version          int32      `json:"version"`
	RevokedAt        *time.Time `json:"revoked_at"`
	RevokedBy        *uuid.UUID `json:"revoked_by"`
	RevocationReason *string    `json:"revocation_reason"`
}

type History struct {
	ID          uuid.UUID       `json:"id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	Action      string          `json:"action"`
	At          time.Time       `json:"at"`
	Metadata    json.RawMessage `json:"metadata"`
}

type ScopedID struct{ BusinessID, ID uuid.UUID }
type Page struct{ Limit, Offset int32 }
type GrantPage struct {
	Items  []Grant `json:"items"`
	Total  int32   `json:"total"`
	Limit  int32   `json:"limit"`
	Offset int32   `json:"offset"`
}
type HistoryPage struct {
	Items  []History `json:"items"`
	Limit  int32     `json:"limit"`
	Offset int32     `json:"offset"`
}
type Creation struct {
	BusinessID, ActorID uuid.UUID
	Input               CreateInput
}
type Revocation struct {
	Key     ScopedID
	ActorID uuid.UUID
	Version int32
	Reason  string
}
type Extension struct {
	Key       ScopedID
	Version   int32
	ExpiresAt *time.Time
}
type Change struct {
	BusinessID, GrantID, ActorID uuid.UUID
	Action, Reason               string
	Before, After                *Grant
}
