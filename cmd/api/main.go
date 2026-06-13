package main

import (
	"context"
	"log"

	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/platform/logger"
	"github.com/palmieridev/openlocal-api/internal/platform/postgres"
	"github.com/palmieridev/openlocal-api/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	logg := logger.New(cfg.AppEnv)
	pool, err := postgres.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	authMiddleware := auth.Middleware{
		Verifier:      auth.NewVerifier(cfg.ClerkIssuerURL, cfg.ClerkJWKSURL),
		AllowTestAuth: cfg.AuthTestBypass,
	}
	app := server.New(server.Deps{
		Config: cfg,
		Logger: logg,
		Pool:   pool,
		Auth:   authMiddleware,
	})
	logg.Info("starting Openlocal API", "addr", cfg.HTTPAddr)
	if err := app.Listen(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
