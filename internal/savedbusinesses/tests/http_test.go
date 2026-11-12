package savedbusinesses_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses"
)

func TestSavedBusinessesLifecycleAndUserIsolation(t *testing.T) {
	f := setup(t)
	b := f.business(t, "active")
	second := f.business(t, "active")
	path := "/me/saved-businesses/" + b.ID.String()
	for range 2 {
		f.request(t, "PUT", path, "", 200)
	}
	f.request(t, "PUT", "/me/saved-businesses/"+second.ID.String(), "", 200)
	first := decode[api.CursorPage[savedbusinesses.SavedBusiness]](t, f.request(t, "GET", "/me/saved-businesses?limit=1", "", 200))
	if len(first.Items) != 1 || !first.Items[0].Available || first.NextCursor == nil {
		t.Fatalf("first page %+v", first)
	}
	next := decode[api.CursorPage[savedbusinesses.SavedBusiness]](t, f.request(t, "GET", "/me/saved-businesses?limit=1&cursor="+*first.NextCursor, "", 200))
	if len(next.Items) != 1 || next.Items[0].BusinessID == first.Items[0].BusinessID || next.NextCursor != nil {
		t.Fatalf("next page %+v", next)
	}
	owner := f.identity
	other, err := f.q.UpsertUserFromClerk(context.Background(), db.UpsertUserFromClerkParams{ClerkUserID: "other-saved-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.q.DeleteUserByClerkID(context.Background(), other.ClerkUserID); err != nil {
			t.Error(err)
		}
	})
	f.identity = auth.Context{ClerkUserID: other.ClerkUserID, UserID: other.ID}
	f.request(t, "GET", "/me/saved-businesses?cursor="+*first.NextCursor, "", 400)
	state := decode[savedbusinesses.SavedState](t, f.request(t, "GET", path, "", 200))
	if state.Saved {
		t.Fatal("foreign user's save exposed")
	}
	f.request(t, "DELETE", path, "", 204)
	empty := decode[api.CursorPage[savedbusinesses.SavedBusiness]](t, f.request(t, "GET", "/me/saved-businesses", "", 200))
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("other list %+v", empty)
	}
	f.identity = owner
	if _, err := f.pool.Exec(context.Background(), "UPDATE businesses SET status='archived' WHERE id=$1", b.ID); err != nil {
		t.Fatal(err)
	}
	page := decode[api.CursorPage[savedbusinesses.SavedBusiness]](t, f.request(t, "GET", "/me/saved-businesses", "", 200))
	for _, item := range page.Items {
		if item.BusinessID == b.ID && (item.Available || item.Name != nil || item.Slug != nil || item.LogoURL != nil) {
			t.Fatal("unavailable storefront details exposed")
		}
	}
	f.request(t, "PUT", path, "", 404)
	f.request(t, "PUT", "/me/saved-businesses/"+uuid.NewString(), "", 404)
	for range 2 {
		f.request(t, "DELETE", path, "", 204)
	}
}
func TestSavedBusinessRejectsInputWithoutWrites(t *testing.T) {
	f := setup(t)
	b := f.business(t, "active")
	path := "/me/saved-businesses/" + b.ID.String()
	f.request(t, "PUT", path, `{}`, 400)
	state := decode[savedbusinesses.SavedState](t, f.request(t, "GET", path, "", 200))
	if state.Saved {
		t.Fatal("invalid body wrote save")
	}
	f.request(t, "GET", "/me/saved-businesses?limit=101", "", 400)
	f.identity = auth.Context{}
	f.request(t, "PUT", path, "", 401)
}
