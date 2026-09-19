package main

import (
	"context"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/jobs"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/platform/logger"
	"github.com/palmieridev/openlocal-api/internal/platform/postgres"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, e := config.Load()
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, e := postgres.Connect(ctx, cfg.DatabaseURL)
	if e != nil {
		log.Fatal(e)
	}
	defer pool.Close()
	logger := logger.New(cfg.AppEnv)
	rt := api.NewRuntime(logger, pool, nil)
	billingHandler := billing.NewHandler(rt, cfg)
	worker := jobs.Worker{Q: rt.Q, Operations: operations.NewHandler(rt, cfg), Reconcile: billingHandler.Reconcile, Logger: logger}
	if e = worker.Run(ctx); e != nil {
		log.Fatal(e)
	}
}
