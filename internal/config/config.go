package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	DatabaseURL            string
	ClerkSecretKey         string
	ClerkAPIURL            string
	ClerkWebhookSecret     string
	ClerkIssuerURL         string
	ClerkJWKSURL           string
	ClerkAuthorizedParties []string
	CORSAllowedOrigins     []string
	AuthTestBypass         bool
	RateLimitWindow        time.Duration
	GlobalRateLimitMax     int
	PublicRateLimitMax     int
	PrivateRateLimitMax    int
	WebhookRateLimitMax    int
}

func Load() (Config, error) {
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load()

	cfg := Config{
		AppEnv:                 getenv("APP_ENV", "development"),
		HTTPAddr:               portAddr(),
		DatabaseURL:            getenv("DATABASE_URL", "postgres://openlocal:openlocal@localhost:5432/openlocal?sslmode=disable"),
		ClerkSecretKey:         os.Getenv("CLERK_SECRET_KEY"),
		ClerkAPIURL:            getenv("CLERK_API_URL", "https://api.clerk.com/v1"),
		ClerkWebhookSecret:     os.Getenv("CLERK_WEBHOOK_SIGNING_SECRET"),
		ClerkIssuerURL:         os.Getenv("CLERK_ISSUER_URL"),
		ClerkJWKSURL:           os.Getenv("CLERK_JWKS_URL"),
		ClerkAuthorizedParties: splitCSV(os.Getenv("CLERK_AUTHORIZED_PARTIES")),
		CORSAllowedOrigins:     splitCSV(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		AuthTestBypass:         getbool("AUTH_TEST_BYPASS", false),
	}
	var err error
	if cfg.RateLimitWindow, err = positiveDuration("RATE_LIMIT_WINDOW", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.GlobalRateLimitMax, err = positiveInt("GLOBAL_RATE_LIMIT_MAX", 600); err != nil {
		return Config{}, err
	}
	if cfg.PublicRateLimitMax, err = positiveInt("PUBLIC_RATE_LIMIT_MAX", 120); err != nil {
		return Config{}, err
	}
	if cfg.PrivateRateLimitMax, err = positiveInt("PRIVATE_RATE_LIMIT_MAX", 300); err != nil {
		return Config{}, err
	}
	if cfg.WebhookRateLimitMax, err = positiveInt("WEBHOOK_RATE_LIMIT_MAX", 60); err != nil {
		return Config{}, err
	}
	if cfg.ClerkIssuerURL == "" || cfg.ClerkJWKSURL == "" {
		if issuer, err := issuerFromPublishableKey(os.Getenv("CLERK_PUBLISHABLE_KEY")); err == nil {
			cfg.ClerkIssuerURL = first(cfg.ClerkIssuerURL, issuer)
			cfg.ClerkJWKSURL = first(cfg.ClerkJWKSURL, issuer+"/.well-known/jwks.json")
		}
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if !cfg.AuthTestBypass && (cfg.ClerkIssuerURL == "" || cfg.ClerkJWKSURL == "") {
		return errors.New("CLERK_ISSUER_URL and CLERK_JWKS_URL are required unless AUTH_TEST_BYPASS=true")
	}
	if !cfg.AuthTestBypass && cfg.ClerkSecretKey == "" {
		return errors.New("CLERK_SECRET_KEY is required unless AUTH_TEST_BYPASS=true")
	}
	if !isProduction(cfg.AppEnv) {
		return nil
	}
	if cfg.AuthTestBypass {
		return errors.New("AUTH_TEST_BYPASS cannot be enabled in production")
	}
	if len(cfg.ClerkAuthorizedParties) == 0 {
		return errors.New("CLERK_AUTHORIZED_PARTIES is required in production")
	}
	if cfg.ClerkWebhookSecret == "" {
		return errors.New("CLERK_WEBHOOK_SIGNING_SECRET is required in production")
	}
	for _, origin := range cfg.CORSAllowedOrigins {
		if origin == "*" {
			return errors.New("wildcard CORS origins are not allowed in production")
		}
	}
	return nil
}

func isProduction(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "production" || value == "prod"
}

func issuerFromPublishableKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	key = strings.TrimPrefix(key, "pk_test_")
	key = strings.TrimPrefix(key, "pk_live_")
	if key == "" {
		return "", errors.New("empty Clerk publishable key")
	}
	decoded, err := decodeBase64String(key)
	if err != nil {
		return "", err
	}
	host := strings.TrimSuffix(string(decoded), "$")
	if host == "" || strings.ContainsAny(host, "/ \t\r\n") {
		return "", fmt.Errorf("invalid Clerk publishable key host")
	}
	return "https://" + host, nil
}

func decodeBase64String(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
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

func portAddr() string {
	if value := strings.TrimSpace(os.Getenv("PORT")); value != "" {
		return ":" + strings.TrimPrefix(value, ":")
	}
	return ":8080"
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

func positiveInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func positiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Second {
		return 0, fmt.Errorf("%s must be a duration of at least one second", key)
	}
	return parsed, nil
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
