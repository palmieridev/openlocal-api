package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses"
)

func TestSavedBusinessesLifecycle(t *testing.T) {
	f := setupOperations(t)
	// The private group already enforces authentication; register before its catch-all middleware.
	savedbusinesses.NewHandler(f.h.RT).RegisterRoutes(f.app.Group("/api/v1"))
	path := "/me/saved-businesses/" + f.b.ID.String()
	other := f
	other.user = "saved-other-" + uuid.NewString()
	other.org = ""
	t.Cleanup(func() { _ = f.h.RT.Q.DeleteUserByClerkID(context.Background(), other.user) })

	for i := 0; i < 2; i++ {
		code, data := f.request("PUT", path, "", nil, true)
		if code != 200 {
			t.Fatalf("save %d %s", code, data)
		}
	}
	code, data := f.request("GET", "/me/saved-businesses?limit=1", "", nil, true)
	if code != 200 {
		t.Fatalf("list %d %s", code, data)
	}
	var page api.CursorPage[savedbusinesses.SavedBusiness]
	mustOperations(t, json.Unmarshal(data, &page))
	if len(page.Items) != 1 || !page.Items[0].Available || page.NextCursor != nil {
		t.Fatalf("page %s", data)
	}

	code, data = other.request("GET", path, "", nil, true)
	if code != 200 || !bytes.Contains(data, []byte(`"saved":false`)) {
		t.Fatalf("saved isolation %d %s", code, data)
	}
	code, data = other.request("GET", "/me/saved-businesses", "", nil, true)
	var empty api.CursorPage[savedbusinesses.SavedBusiness]
	mustOperations(t, json.Unmarshal(data, &empty))
	if code != 200 || len(empty.Items) != 0 {
		t.Fatalf("list isolation %d %s", code, data)
	}
	_, e := f.h.RT.Pool.Exec(context.Background(), "UPDATE businesses SET status='archived' WHERE id=$1", f.b.ID)
	mustOperations(t, e)
	code, data = f.request("GET", "/me/saved-businesses", "", nil, true)
	mustOperations(t, json.Unmarshal(data, &page))
	if code != 200 || page.Items[0].Available || page.Items[0].Name != nil {
		t.Fatalf("unavailable %s", data)
	}
	for i := 0; i < 2; i++ {
		code, data = f.request("DELETE", path, "", nil, true)
		if code != 204 {
			t.Fatalf("delete %d %s", code, data)
		}
	}
}
