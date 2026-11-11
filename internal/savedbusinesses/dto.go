package savedbusinesses

import (
	"time"

	"github.com/google/uuid"
)

type SavedBusiness struct {
	BusinessID uuid.UUID `json:"business_id"`
	SavedAt    time.Time `json:"saved_at"`
	Available  bool      `json:"available"`
	Name       *string   `json:"name"`
	Slug       *string   `json:"slug"`
	LogoURL    *string   `json:"logo_url"`
}
type SavedState struct {
	Saved bool `json:"saved"`
}
