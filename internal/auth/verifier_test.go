package auth

import (
	"encoding/json"
	"testing"
)

func TestApplyOrgFallbackReadsV2NestedClaim(t *testing.T) {
	// Clerk v2 session token: active org nested under `o`, no flat org_id.
	var claims Claims
	if err := json.Unmarshal([]byte(`{"sub":"user_1","o":{"id":"org_123","rol":"admin","slg":"acme"}}`), &claims); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	claims.applyOrgFallback()
	if claims.OrgID != "org_123" {
		t.Fatalf("expected OrgID org_123, got %q", claims.OrgID)
	}
	if claims.OrgRole != "admin" {
		t.Fatalf("expected OrgRole admin, got %q", claims.OrgRole)
	}
	if got := MapClerkRole(claims.OrgRole); got != "owner" {
		t.Fatalf("expected mapped role owner, got %q", got)
	}
}

func TestApplyOrgFallbackPrefersFlatV1Claim(t *testing.T) {
	// When both are present (v1 flat + v2 nested), keep the flat v1 values.
	var claims Claims
	if err := json.Unmarshal([]byte(`{"sub":"user_1","org_id":"org_flat","org_role":"org:member","o":{"id":"org_nested","rol":"admin"}}`), &claims); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	claims.applyOrgFallback()
	if claims.OrgID != "org_flat" {
		t.Fatalf("expected OrgID org_flat, got %q", claims.OrgID)
	}
	if claims.OrgRole != "org:member" {
		t.Fatalf("expected OrgRole org:member, got %q", claims.OrgRole)
	}
}

func TestVerifyAuthorizedPartyAllowsConfiguredOrigin(t *testing.T) {
	verifier := NewVerifier("https://issuer.example", "https://issuer.example/.well-known/jwks.json", []string{"http://localhost:3000/"})
	if err := verifier.verifyAuthorizedParty("http://localhost:3000"); err != nil {
		t.Fatalf("expected authorized party to pass: %v", err)
	}
}

func TestVerifyAuthorizedPartyRejectsMissingWhenConfigured(t *testing.T) {
	verifier := NewVerifier("https://issuer.example", "https://issuer.example/.well-known/jwks.json", []string{"http://localhost:3000"})
	if err := verifier.verifyAuthorizedParty(""); err == nil {
		t.Fatal("expected missing authorized party to fail")
	}
}

func TestVerifyAuthorizedPartyRejectsUnknownOrigin(t *testing.T) {
	verifier := NewVerifier("https://issuer.example", "https://issuer.example/.well-known/jwks.json", []string{"http://localhost:3000"})
	if err := verifier.verifyAuthorizedParty("https://evil.example"); err == nil {
		t.Fatal("expected unknown authorized party to fail")
	}
}
