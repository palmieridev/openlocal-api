package users

import (
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type MeResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     *string   `json:"email,omitempty"`
	FirstName *string   `json:"first_name,omitempty"`
	LastName  *string   `json:"last_name,omitempty"`
	ImageURL  *string   `json:"image_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func mapUser(user db.User) MeResponse {
	return MeResponse{
		ID:        user.ID,
		Email:     api.StringPtr(user.Email),
		FirstName: api.StringPtr(user.FirstName),
		LastName:  api.StringPtr(user.LastName),
		ImageURL:  api.StringPtr(user.ImageUrl),
		CreatedAt: api.TS(user.CreatedAt),
		UpdatedAt: api.TS(user.UpdatedAt),
	}
}
