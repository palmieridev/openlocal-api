include .env

.PHONY: test sqlc migrate-up migrate-down run db-up db-down db-logs db-ps db-reset

COMPOSE ?= podman-compose

test:
	go test ./...

sqlc:
	sqlc generate

migrate-up:
	migrate -path db/migrations -database "${DATABASE_URL}" up

migrate-down:
	migrate -path db/migrations -database "${DATABASE_URL}" down 1

run:
	go run ./cmd/api

db-up:
	$(COMPOSE) up -d postgres

db-stop:
	$(COMPOSE) stop postgres

db-logs:
	$(COMPOSE) logs -f postgres

db-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up -d postgres
