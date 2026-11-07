// HTTP smoke checks through the real Fiber router, without listening on a port
// or redirecting a running service. Pickup-window GET may materialize demo windows.
// Private requests remain gated.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/server"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, "Demo HTTP verification failed:", err)
		os.Exit(1)
	}
}
func check() error {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration invalid")
	}
	if cfg.AppEnv != "development" || cfg.AuthTestBypass {
		return fmt.Errorf("requires development without auth bypass")
	}
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database config invalid")
	}
	// Reuse credentials inside the existing API container; no URL leaves it.
	if pc.ConnConfig.Host != "postgres" {
		return fmt.Errorf("requires local compose postgres host")
	}
	pc.ConnConfig.Database = "openlocal_demo"
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return fmt.Errorf("cannot open demo database")
	}
	defer pool.Close()
	var marker string
	if err = pool.QueryRow(ctx, "SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()").Scan(&marker); err != nil || marker != "openlocal-demo-v1-local-only" {
		return fmt.Errorf("demo marker missing")
	}
	cfg.GlobalRateLimitMax = 1000
	cfg.PublicRateLimitMax = 1000
	app := server.New(server.Deps{Config: cfg, Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: auth.Middleware{Verifier: auth.NewVerifier(cfg.ClerkIssuerURL, cfg.ClerkJWKSURL, cfg.ClerkAuthorizedParties)}})
	date := time.Now().In(mustZone()).AddDate(0, 0, 1).Format("2006-01-02")
	var serviceID string
	if err = pool.QueryRow(ctx, "SELECT id FROM services WHERE business_id=(SELECT id FROM businesses WHERE slug='demo-salon') AND name='Corte y peinado'").Scan(&serviceID); err != nil {
		return err
	}
	paths := []string{"/healthz", "/api/v1/marketplace/businesses?limit=100", "/api/v1/public/businesses/demo-tienda", "/api/v1/public/businesses/demo-tienda/products", "/api/v1/public/businesses/demo-pan", "/api/v1/public/businesses/demo-salon", "/api/v1/public/businesses/demo-pan/preorders/catalog", "/api/v1/public/businesses/demo-pan/preorders/pickup-windows?date=" + date, "/api/v1/public/businesses/demo-salon/appointments/services", "/api/v1/public/businesses/demo-salon/appointments/availability?date=" + date + "&service_id=" + serviceID}
	for _, path := range paths {
		res, e := app.Test(httptest.NewRequest("GET", path, nil), 10000)
		if e != nil {
			return e
		}
		body, e := io.ReadAll(res.Body)
		res.Body.Close()
		if e != nil {
			return e
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("%s returned %d: %s", path, res.StatusCode, body)
		}
		var value any
		if e = json.Unmarshal(body, &value); e != nil {
			return e
		}
		if path != "/healthz" {
			if err := publicFields(value); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		if object, ok := value.(map[string]any); ok {
			for _, key := range []string{"slots", "windows", "items"} {
				if list, exists := object[key]; exists {
					if rows, ok := list.([]any); !ok || len(rows) == 0 {
						return fmt.Errorf("%s has no %s", path, key)
					}
				}
			}
		}
		if len(body) < 3 || string(body) == "[]" {
			return fmt.Errorf("%s returned empty data", path)
		}
		fmt.Println("PASS GET", path)
	}
	res, err := app.Test(httptest.NewRequest("GET", "/api/v1/businesses/me", nil))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		return fmt.Errorf("private API must reject unauthenticated requests")
	}
	fmt.Println("PASS private auth remains enforced (401)")
	return nil
}
func mustZone() *time.Location {
	z, e := time.LoadLocation("America/Mexico_City")
	if e != nil {
		panic(e)
	}
	return z
}

// Inspect decoded keys, so nested arrays/projections cannot leak private fields.
func publicFields(v any) error {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			switch k {
			case "cost", "unit_cost", "clerk_user_id", "clerk_org_id", "quantity_on_hand", "quantity_reserved", "supplier_notes":
				return fmt.Errorf("private field %s exposed", k)
			}
			if err := publicFields(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := publicFields(child); err != nil {
				return err
			}
		}
	}
	return nil
}
