package identity_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestAuthUpsertPreservesLegacyProfileAndReturnContract(t *testing.T) {
	p := testPool(t)
	q := db.New(p)
	ctx := context.Background()
	id := "user_" + uuid.NewString()
	trackUser(t, p, id)
	original := auth.Context{ClerkUserID: id, ClerkOrgID: "org_verified", Role: "owner", Email: "", FirstName: " First ", LastName: "Last", ImageURL: "https://example.test/image"}
	updated, u, err := original.UpsertUser(ctx, q)
	must(t, err)
	if updated.UserID != u.ID || u.ID == uuid.Nil || updated.ClerkOrgID != original.ClerkOrgID || updated.Role != original.Role {
		t.Fatal("auth context changed")
	}
	stored, err := q.GetUserByClerkID(ctx, id)
	must(t, err)
	if u.ClerkUserID != id || u.FirstName.String != " First " || u.Email.Valid || !u.FirstName.Valid || u.ImageUrl.String != original.ImageURL || !u.CreatedAt.Valid || !u.UpdatedAt.Valid || !u.CreatedAt.Time.Equal(stored.CreatedAt.Time) || !u.UpdatedAt.Time.Equal(stored.UpdatedAt.Time) {
		t.Fatalf("legacy row contract changed: %+v", u)
	}
	original.Email = "updated@example.test"
	original.FirstName = ""
	_, again, err := original.UpsertUser(ctx, q)
	must(t, err)
	if again.ID != u.ID || again.FirstName.Valid || again.Email.String != original.Email {
		t.Fatalf("profile update = %+v", again)
	}
}
