package api_test

import (
	"testing"

	"github.com/palmieridev/openlocal-api/internal/api"
)

func TestRoleAllowedRequiresExplicitMatch(t *testing.T) {
	if !api.RoleAllowed("manager", "owner", "manager") {
		t.Fatal("expected manager to be allowed")
	}
	for _, role := range []string{"", "staff", "admin", "owner "} {
		if api.RoleAllowed(role, "owner", "manager") {
			t.Fatalf("expected role %q to be denied", role)
		}
	}
}
