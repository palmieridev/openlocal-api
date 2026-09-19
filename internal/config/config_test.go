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

func TestValidateRequiresOperationalSecurityAndCompleteStripeConfig(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	base := Config{AppEnv: "production", ClerkSecretKey: "secret", ClerkWebhookSecret: "webhook", ClerkIssuerURL: "https://issuer.example", ClerkJWKSURL: "https://issuer.example/.well-known/jwks.json", ClerkAuthorizedParties: []string{"https://openlocal.store"}, CORSAllowedOrigins: []string{"https://openlocal.store"}, ModulesTokenKey: key, TurnstileSecret: "turnstile", TurnstileHostnames: []string{"openlocal.store"}, AppBaseURL: "https://openlocal.store"}
	if err := validate(base); err != nil {
		t.Fatalf("secure production config rejected: %v", err)
	}
	missingKey := base
	missingKey.ModulesTokenKey = ""
	if err := validate(missingKey); err == nil || !strings.Contains(err.Error(), "operational modules") {
		t.Fatalf("missing module key: %v", err)
	}
	partial := base
	partial.StripeSecretKey = "sk_live_test"
	if err := validate(partial); err == nil || !strings.Contains(err.Error(), "all Stripe") {
		t.Fatalf("partial Stripe config: %v", err)
	}
	complete := base
	complete.StripeSecretKey = "sk_live_test"
	complete.StripeWebhookSecret = "whsec_test"
	complete.StripeEssentialPrice = "price_e"
	complete.StripeBusinessPrice = "price_b"
	complete.StripeSecondModulePrice = "price_a"
	complete.StripePortalConfiguration = "bpc_test"
	if err := validate(complete); err != nil {
		t.Fatalf("complete Stripe config rejected: %v", err)
	}
}

func TestPortAddrUsesPort(t *testing.T) {
	t.Setenv("PORT", "8080")

	if got := portAddr(); got != ":8080" {
		t.Fatalf("portAddr() = %q, want %q", got, ":8080")
	}
}

func TestPortAddrAcceptsColonPrefixedPort(t *testing.T) {
	t.Setenv("PORT", ":9000")

	if got := portAddr(); got != ":9000" {
		t.Fatalf("portAddr() = %q, want %q", got, ":9000")
	}
}

func TestPortAddrDefaultsTo8080(t *testing.T) {
	t.Setenv("PORT", "")

	if got := portAddr(); got != ":8080" {
		t.Fatalf("portAddr() = %q, want %q", got, ":8080")
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
