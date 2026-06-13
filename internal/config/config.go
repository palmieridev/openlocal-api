package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv             string
	HTTPAddr           string
	DatabaseURL        string
	ClerkIssuerURL     string
	ClerkJWKSURL       string
	CORSAllowedOrigins []string
	AuthTestBypass     bool
}

func Load() (Config, error) {
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load()

	cfg := Config{
		AppEnv:             getenv("APP_ENV", "development"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://openlocal:openlocal@localhost:5432/openlocal?sslmode=disable"),
		ClerkIssuerURL:     os.Getenv("CLERK_ISSUER_URL"),
		ClerkJWKSURL:       os.Getenv("CLERK_JWKS_URL"),
		CORSAllowedOrigins: splitCSV(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		AuthTestBypass:     getbool("AUTH_TEST_BYPASS", false),
	}
	if cfg.ClerkIssuerURL == "" || cfg.ClerkJWKSURL == "" {
		if issuer, err := issuerFromPublishableKey(os.Getenv("CLERK_PUBLISHABLE_KEY")); err == nil {
			cfg.ClerkIssuerURL = first(cfg.ClerkIssuerURL, issuer)
			cfg.ClerkJWKSURL = first(cfg.ClerkJWKSURL, issuer+"/.well-known/jwks.json")
		}
	}

	if !cfg.AuthTestBypass && (cfg.ClerkIssuerURL == "" || cfg.ClerkJWKSURL == "") {
		return Config{}, errors.New("CLERK_ISSUER_URL and CLERK_JWKS_URL are required unless AUTH_TEST_BYPASS=true")
	}
	return cfg, nil
}

func issuerFromPublishableKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	key = strings.TrimPrefix(key, "pk_test_")
	key = strings.TrimPrefix(key, "pk_live_")
	if key == "" {
		return "", errors.New("empty Clerk publishable key")
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}
	host := strings.TrimSuffix(string(decoded), "$")
	if host == "" || strings.ContainsAny(host, "/ \t\r\n") {
		return "", fmt.Errorf("invalid Clerk publishable key host")
	}
	return "https://" + host, nil
}

func first(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getbool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
