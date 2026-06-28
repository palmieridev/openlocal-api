include .env

.PHONY: test start sqlc migrate-up migrate-down seed run db-up db-stop db-down db-logs db-ps db-reset

COMPOSE ?= podman-compose

test:
	go test ./...

sqlc:
	sqlc generate

migrate-up:
	migrate -path db/migrations -database "${DATABASE_URL}" up

migrate-down:
	migrate -path db/migrations -database "${DATABASE_URL}" down 1

seed:
	$(COMPOSE) exec -T postgres psql -U "$(PGUSER)" -d "$(PGDATABASE)" < db/seeds/marketplace.sql

run:
	go run ./cmd/api

start:
	$(COMPOSE) start
db-up:
	$(COMPOSE) up -d postgres

db-stop:
	$(COMPOSE) stop postgres

db-logs:
	$(COMPOSE) logs -f postgres

db-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d postgres
