package api

import "testing"

func TestRoleAllowedRequiresExplicitMatch(t *testing.T) {
	if !RoleAllowed("manager", "owner", "manager") {
		t.Fatal("expected manager to be allowed")
	}
	for _, role := range []string{"", "staff", "admin", "owner "} {
		if RoleAllowed(role, "owner", "manager") {
			t.Fatalf("expected role %q to be denied", role)
		}
	}
}
