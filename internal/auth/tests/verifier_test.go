package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifierResolvesVerifiedOrganizationClaims(t *testing.T) {
	verifier, key := newVerifier(t, []string{"https://shop.example"})
	for _, tc := range []struct {
		name             string
		org              jwt.MapClaims
		wantID, wantRole string
	}{
		{"v2 nested", jwt.MapClaims{"o": map[string]string{"id": "org_nested", "rol": "admin"}}, "org_nested", "admin"},
		{"v1 takes precedence", jwt.MapClaims{"org_id": "org_flat", "org_role": "org:member", "o": map[string]string{"id": "org_nested", "rol": "admin"}}, "org_flat", "org:member"},
		{"missing role uses nested role", jwt.MapClaims{"org_id": "org_flat", "o": map[string]string{"id": "org_nested", "rol": "admin"}}, "org_flat", "admin"},
		{"no active org", jwt.MapClaims{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://issuer.example", "sub": "user_fixture", "exp": time.Now().Add(time.Hour).Unix(), "azp": "https://shop.example"}
			for field, value := range tc.org {
				claims[field] = value
			}
			got, err := verifier.Verify(context.Background(), signedClaims(t, key, claims))
			if err != nil {
				t.Fatal(err)
			}
			if got.OrgID != tc.wantID || got.OrgRole != tc.wantRole {
				t.Fatalf("organization=%q role=%q, want %q/%q", got.OrgID, got.OrgRole, tc.wantID, tc.wantRole)
			}
		})
	}
}

func TestVerifierEnforcesAuthorizedPartyOnSignedTokens(t *testing.T) {
	verifier, key := newVerifier(t, []string{"https://shop.example/"})
	for _, tc := range []struct {
		name, party string
		allowed     bool
	}{
		{"configured origin", "https://shop.example", true},
		{"normalized origin", " https://shop.example/ ", true},
		{"missing", "", false},
		{"foreign origin", "https://evil.example", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": "https://issuer.example", "sub": "user_fixture", "exp": time.Now().Add(time.Hour).Unix(), "azp": tc.party}
			_, err := verifier.Verify(context.Background(), signedClaims(t, key, claims))
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, verification error=%v", tc.allowed, err)
			}
		})
	}
}
