# Openlocal API

Openlocal API is a Go/Fiber/PostgreSQL backend for local commerce: business profiles, storefront catalogs, inventory movements, stock levels, marketplace discovery, and inventory analytics.

## Stack

- Go + Fiber
- PostgreSQL with `pgx/v5`
- `sqlc` for typed queries
- `golang-migrate` migrations
- Clerk authentication, with one Clerk organization per business
- Docker Compose for local Postgres

## Local Setup

```sh
make db-up
migrate -path db/migrations -database "$DATABASE_URL" up
make seed
sqlc generate
go test ./...
go run ./cmd/api
```
