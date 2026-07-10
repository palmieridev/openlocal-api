package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestIssuerFromPublishableKeyAcceptsUnpaddedClerkKey(t *testing.T) {
	host := "example.clerk.accounts.dev"
	key := "pk_test_" + base64.RawStdEncoding.EncodeToString([]byte(host+"$"))

	issuer, err := issuerFromPublishableKey(key)
	if err != nil {
		t.Fatalf("issuerFromPublishableKey returned error: %v", err)
	}

	if issuer != "https://"+host {
		t.Fatalf("issuer = %q, want %q", issuer, "https://"+host)
	}
}

func TestValidateRejectsUnsafeProductionConfiguration(t *testing.T) {
	base := Config{
		AppEnv:                 "production",
		ClerkSecretKey:         "secret",
		ClerkWebhookSecret:     "webhook",
		ClerkIssuerURL:         "https://issuer.example",
		ClerkJWKSURL:           "https://issuer.example/.well-known/jwks.json",
		ClerkAuthorizedParties: []string{"https://app.example"},
		CORSAllowedOrigins:     []string{"https://app.example"},
	}
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"test auth bypass", func(cfg *Config) { cfg.AuthTestBypass = true }, "AUTH_TEST_BYPASS"},
		{"missing authorized parties", func(cfg *Config) { cfg.ClerkAuthorizedParties = nil }, "CLERK_AUTHORIZED_PARTIES"},
		{"missing webhook secret", func(cfg *Config) { cfg.ClerkWebhookSecret = "" }, "CLERK_WEBHOOK_SIGNING_SECRET"},
		{"wildcard cors", func(cfg *Config) { cfg.CORSAllowedOrigins = []string{"*"} }, "wildcard CORS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)
			err := validate(cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestIssuerFromPublishableKeyAcceptsPaddedClerkKey(t *testing.T) {
	host := "example.clerk.accounts.dev"
	key := "pk_live_" + base64.StdEncoding.EncodeToString([]byte(host+"$"))

	issuer, err := issuerFromPublishableKey(key)
	if err != nil {
		t.Fatalf("issuerFromPublishableKey returned error: %v", err)
	}

	if issuer != "https://"+host {
		t.Fatalf("issuer = %q, want %q", issuer, "https://"+host)
	}
}
