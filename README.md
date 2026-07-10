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

## Security Defaults

- Clerk bearer tokens are verified against issuer, JWKS, expiry, and configured authorized parties.
- Business authorization requires both the active Clerk organization role and the matching local membership role.
- Production startup rejects test-auth bypass, wildcard CORS, missing authorized parties, and a missing Clerk webhook secret.
- Requests use strict JSON decoding, a 1 MiB body limit, bounded pagination, domain validation, and parameterized sqlc queries.
- Global, public, private, and webhook rate limits are configurable through `.env.example`. The built-in limiter is per process; use a shared edge or Redis-backed limiter before running multiple API replicas.
- Stock movement writes require an `Idempotency-Key` header and update the movement ledger and stock level in one transaction.

## Tests

- Keep pure validation and mapping tests beside their domain package (`internal/catalog`, `internal/inventory`, and similar).
- Keep middleware and HTTP security behavior tests in `internal/server` and `internal/auth`.
- Keep database transaction and tenant-isolation tests in `internal/integration`; they are opt-in and roll back their fixtures.

```sh
go test ./...
go test -race ./...
OPENLOCAL_INTEGRATION_TESTS=1 go test ./internal/integration
```
