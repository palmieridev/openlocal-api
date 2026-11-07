package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/server"
)

func TestGrantRoutesWithFoundationAndOwnerSubscription(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	q := db.New(pool)
	admin := createUser(t, ctx, q)
	owner := createUser(t, ctx, q)
	business := createBusiness(t, ctx, q)
	other := createBusiness(t, ctx, q)
	org := "org_grants_" + uuid.NewString()
	if _, err := pool.Exec(ctx, "UPDATE businesses SET clerk_org_id=$1 WHERE id=$2", org, business.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: business.ID, UserID: owner.ID, ClerkOrgID: org, Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM audit_logs WHERE business_id IN ($1,$2)", business.ID, other.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM businesses WHERE id IN ($1,$2)", business.ID, other.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id IN ($1,$2)", admin.ID, owner.ID)
	})
	cfg := config.Config{AppEnv: "test", PlatformAdminUserIDs: []string{admin.ClerkUserID}, GlobalRateLimitMax: 1000, PublicRateLimitMax: 1000, PrivateRateLimitMax: 1000, WebhookRateLimitMax: 1000, RateLimitWindow: time.Minute}
	app := server.New(server.Deps{Config: cfg, Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.Middleware{AllowTestAuth: true}})
	request := func(method, path string, body any, user string, want int) []byte {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Clerk-User-ID", user)
		if user == owner.ClerkUserID {
			req.Header.Set("X-Test-Clerk-Org-ID", org)
			req.Header.Set("X-Test-Clerk-Org-Role", "org:admin")
		}
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, res.StatusCode, want, out)
		}
		if want < 300 && res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("private response can be cached")
		}
		return out
	}
	base := "/admin/businesses/" + business.ID.String() + "/grants"
	end := time.Now().Add(time.Hour)
	input := accessgrants.CreateInput{Module: "appointments", Tier: "business", StartsAt: time.Now().Add(-time.Minute), ExpiresAt: &end, InternalReason: "secret reason", OwnerMessage: func() *string { s := "Enjoy the gift"; return &s }()}
	data := request("POST", base, input, admin.ClerkUserID, 201)
	var g accessgrants.Grant
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	if g.CreatedBy == nil || *g.CreatedBy != admin.ID || g.Status != "active" || g.Version != 1 {
		t.Fatalf("grant %s", data)
	}
	// Foundation inspection and grants coexist; explicit projection retains effective sources.
	detail := request("GET", "/admin/businesses/"+business.ID.String(), nil, admin.ClerkUserID, 200)
	if !bytes.Contains(detail, []byte(`"kind":"grant"`)) || !bytes.Contains(detail, []byte(`"tier":"business"`)) {
		t.Fatalf("foundation projection %s", detail)
	}
	for _, path := range []string{"/businesses/" + business.ID.String() + "/entitlements", "/businesses/" + business.ID.String() + "/subscription"} {
		out := request("GET", path, nil, owner.ClerkUserID, 200)
		if !bytes.Contains(out, []byte("Enjoy the gift")) || !bytes.Contains(out, []byte(`"kind":"grant"`)) || bytes.Contains(out, []byte("secret reason")) || bytes.Contains(out, []byte("internal_reason")) {
			t.Fatalf("owner projection %s", out)
		}
	}
	// Invalid enums, dates, JSON shape, nullable/indefinite ambiguity and scope.
	for _, body := range []any{nil, map[string]any{"module": "pickup"}, map[string]any{"module": "appointments", "tier": "business", "starts_at": time.Now(), "internal_reason": "why"}, map[string]any{"module": "appointments", "tier": "business", "starts_at": time.Now(), "expires_at": end, "indefinite": true, "internal_reason": "why"}, map[string]any{"module": "appointments", "tier": "business", "starts_at": time.Now(), "indefinite": true, "internal_reason": "why", "billing": true}} {
		request("POST", base, body, admin.ClerkUserID, 400)
	}
	request("POST", base, input, owner.ClerkUserID, 403)
	request("GET", base+"?limit=101", nil, admin.ClerkUserID, 400)
	request("GET", base+"?offset=-1", nil, admin.ClerkUserID, 400)
	request("GET", base+"?limit=1", nil, admin.ClerkUserID, 200)
	request("POST", "/admin/businesses/"+other.ID.String()+"/grants/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 1, InternalReason: "wrong scope"}, admin.ClerkUserID, 404)
	request("POST", base+"/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 1, ExpiresAt: &end, InternalReason: "same end"}, admin.ClerkUserID, 400)
	out := request("POST", base+"/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 1, Indefinite: true, InternalReason: "indefinite gift"}, admin.ClerkUserID, 200)
	if !bytes.Contains(out, []byte(`"expires_at":null`)) {
		t.Fatalf("expiry %s", out)
	}
	request("POST", base+"/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 2, Indefinite: true, InternalReason: "already indefinite"}, admin.ClerkUserID, 409)
	request("POST", base+"/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 2, InternalReason: "done"}, admin.ClerkUserID, 200)
	request("POST", base+"/"+g.ID.String()+"/extend", accessgrants.ExtendInput{Version: 3, Indefinite: true, InternalReason: "revoked"}, admin.ClerkUserID, 409)
	request("POST", base+"/"+g.ID.String()+"/revoke", accessgrants.RevokeInput{Version: 3, InternalReason: "again"}, admin.ClerkUserID, 409)
	history := request("GET", base+"/"+g.ID.String()+"/history", nil, admin.ClerkUserID, 200)
	if strings.Count(string(history), `"action":"access_grant.`) != 3 {
		t.Fatalf("history %s", history)
	}
	activity := request("GET", "/admin/activity?business_id="+business.ID.String()+"&action=access_grant.revoked", nil, admin.ClerkUserID, 200)
	if !bytes.Contains(activity, []byte(`"before"`)) || !bytes.Contains(activity, []byte(`"after"`)) {
		t.Fatalf("audit %s", activity)
	}
	// Clerk deletion must not fail because a former administrator authored grants.
	if err := q.DeleteUserByClerkID(ctx, admin.ClerkUserID); err != nil {
		t.Fatal(err)
	}
	stored, err := q.GetAccessGrant(ctx, db.GetAccessGrantParams{BusinessID: business.ID, ID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stored.CreatedBy.Valid || stored.RevokedBy.Valid || !stored.RevokedAt.Valid {
		t.Fatal("actor deletion changed grant lifecycle")
	}
}
