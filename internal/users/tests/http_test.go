package users_test

import (
	"testing"

	"github.com/palmieridev/openlocal-api/internal/auth"
)

func TestMeProjectsOnlyAuthenticatedLocalIdentity(t *testing.T) {
	f := setup(t)
	data := f.request(t, "GET", "/me?user_id=another-user", "", 200)
	got := decode[map[string]any](t, data)
	if got["id"] != f.identity.UserID.String() {
		t.Fatalf("wrong identity %s", data)
	}
	for _, key := range []string{"clerk_user_id", "clerk_org_id", "role", "business_id"} {
		if _, ok := got[key]; ok {
			t.Fatalf("leaked %s", key)
		}
	}
	f.identity = auth.Context{}
	f.request(t, "GET", "/me", "", 401)
}
