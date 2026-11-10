# AGENTS.md

## Project Context

Openlocal API (`openlocal-api`) is a Go/Fiber/PostgreSQL backend for local commerce: inventory, storefronts, barcode stock control, public catalogs, marketplace discovery, and analytics.

Canonical names:

- product: `Openlocal`
- repo/module: `openlocal-api`
- Go module: `github.com/palmieridev/openlocal-api`

## Stack

- Go + Fiber
- PostgreSQL with `pgx/v5`
- sqlc generated code in `internal/platform/postgres/db`
- golang-migrate migrations in `db/migrations`
- Clerk auth with JWKS verification
- one Clerk organization per Openlocal business

## External State

Clerk:

- application: `Openlocal API`
- application ID: `app_3F6Bk3hXx7qbn8arLYrVovyU02K`
- development instance: `ins_3F6Bk36XmEtbT2i9XqbPhFQ1eko`
- production instance: `ins_3GLHtVEzdlBIwHaiYI9mMVu5L9M` (domain `openlocal.store`,
  `pk_live_…`; Railway prod holds its secret key)
- `.env.local` was pulled with Clerk keys and is gitignored

GitHub:

- repo: `palmieridev/openlocal-api`
- URL: `https://github.com/palmieridev/openlocal-api`
- origin: `git@github.com:palmieridev/openlocal-api.git`

Do not run `git commit`, `git push`, or `gh repo create --push` unless the user explicitly asks.

## Command Conventions

The user's shell is fish. Use `fish -lc` for env-sensitive Clerk and GitHub commands:

```sh
fish -lc 'clerk doctor --json'
fish -lc 'clerk env pull --instance dev --file .env.local'
fish -lc 'gh repo view palmieridev/openlocal-api'
```

Local development:

```sh
make docker-up
migrate -path db/migrations -database "$DATABASE_URL" up
sqlc generate
go test ./...
go run ./cmd/api
```

## Architecture Rules

- `internal/server` only composes the Fiber app, global middleware, health check, and route registration.
- Shared HTTP/authz helpers live in `internal/api`.
- Domain packages own their routes, handlers, DTOs, and service/domain rules.
- Keep persistence behind sqlc queries.
- Keep domain invariants in domain/service code, not in `internal/server`.
- Do not return raw sqlc structs from public APIs.
- Use request DTOs and response DTOs.
- Public DTOs must be explicit projections.

## Development Strategy: TDD and Security by Design

- Start behavioral changes with explicit invariants and failure scenarios: the authorized actor and business, caller-controlled identifiers, allowed state changes, transaction boundaries, retries, and concurrent requests. Use these requirements to choose tests.
- For bugs and new behavior, follow red → green → refactor: add a focused behavioral test, run it against the current code, and confirm it fails for the expected reason before implementing the fix. Compilation errors, missing infrastructure, and skipped tests do not establish the red step. Preserve the test while improving the implementation.
- During structural refactors, retain contract coverage and add characterization tests where behavior is uncertain. Do not preserve a known security flaw as an accepted contract; establish the secure behavior with a failing regression test and fix it explicitly.
- Enforce tenant isolation for every referenced resource in the application service and persistence boundary. An authorized business ID does not authorize a caller-supplied variant, location, or other resource from another business. Foreign and nonexistent resources should receive the same rejection without revealing their existence, and rejected requests must leave no partial writes.
- Keep new or extracted application services independent of Fiber, `api.Runtime`, pgx, and generated sqlc types. Use explicit service inputs/results and small consumer-owned ports; adapters translate HTTP and database representations. Pass identity authorized by the caller boundary explicitly, and enforce resource ownership inside the use case.
- Use fast domain tests for invariants and validation, and small fakes for application decisions or failure paths. Use real PostgreSQL with migrations for transaction atomicity, constraints, idempotency, and concurrency; mocks cannot prove those guarantees. HTTP tests cover authorization, decoding, status/error contracts, and response projections.
- Assert outcomes and persisted state, including the absence of side effects after rejection or failure. For stock creation, verify movement, stock, and audit writes commit or roll back together; retries apply once; distinct concurrent movements all apply; and default-location resolution is safe under contention.
- Coordinate concurrency tests with explicit barriers or database locks, bounded by timeouts. Observe that the conflicting operations reached the intended overlap before releasing them; goroutine scheduling or fixed sleeps alone are insufficient.
- Use explicit disposable test databases for tests that commit writes or inject failures. Isolate fixtures, clean up resources, and scope triggers or other fault injection to the fixture so concurrently running packages are unaffected. Never use production databases or unverified environment credentials.
- Choose tests by the failures they detect, not by test counts or coverage targets. Confirm security and integrity tests detect removal of the relevant guard or guarantee, using the observed red step or a temporary, reverted mutation when useful. Avoid assertions that merely repeat implementation details.
- CI must explicitly enable required database suites and provision their migrated database. Report unit and integration results separately and call out skipped suites; a passing default `go test ./...` does not prove database coverage. Broaden testing when changes or unresolved risks justify it.

## Test Organization

- Put tests in a `tests/` directory within the owning domain or shared package, for example `internal/catalog/tests` or `internal/platform/validator/tests`. This applies to unit, validation, mapping, HTTP, and domain-specific persistence tests.
- Use external test packages that import production APIs, such as `package catalog_test`. Test stable behavior through public services, adapters, or registered routes so internal file moves and helper changes do not require rewriting assertions. Do not export implementation helpers solely for tests, add production test hooks, or recreate production logic in private test shims.
- Keep test-only helpers and fakes in `*_test.go` files in the test directory, and fixture files in its `testdata/` directory. Keep fixtures scoped and cleaned up; share helpers only when multiple tests need the same setup.
- Reserve `internal/integration` for cross-package flows. A database test for one domain belongs in that domain's `tests/` directory, not automatically in the cross-package integration suite.
- Apply this layout to all new tests and migrate existing tests when refactoring their owning package, preserving meaningful coverage. Use recursive commands such as `go test ./internal/catalog/...` or `go test ./...` so test subpackages actually run; update CI commands when moving tests.

## Documentation Rules

- Reserve `docs/` for API endpoint documentation and its supporting code/specifications. Do not add demo guides, implementation notes, review findings, verification reports, or PR follow-up notes there.
- Report implementation and verification findings in the conversation or PR description. Keep endpoint documentation focused on the current API contract; remove obsolete follow-up notes when the work is implemented.
- Error responses contain `error` and an optional machine-readable `code`. Domain errors include `code`; the current shared error handler omits it for Fiber errors and database not-found errors. Clients must handle a missing `code` using the HTTP status.
- Document `code` as optional. Its absence on these responses is supported behavior, not an outstanding bug or a Fiber limitation; do not add issue/caveat notes presenting it as one.

## Security Rules

- Clerk owns identity, sessions, password policy, and organization membership.
- Private routes require a Clerk bearer token.
- Business routes require local membership for the active Clerk organization.
- Authorization is deny-by-default.
- Validate all input: JSON shape, UUIDs, slugs, enums, decimals, coordinates, pagination.
- Keep list limits bounded. Default is `25`, max is `100`.
- Use sqlc/pgx parameters only. Do not concatenate SQL.
- Never expose product cost, supplier/private notes, exact private stock, stock movement history, or Clerk IDs through public endpoints.
- Stock levels may change only in the same transaction that inserts a stock movement.
- Do not print or commit secrets. `.env.local` must stay ignored.

## Domain Conventions

- **`business_hours.day_of_week` is `0 = Monday` … `6 = Sunday`.** The DB only enforces `0..6`; this is the agreed convention and every client depends on it. Never reinterpret it as Sunday-first.
- Times are stored as `time` (no timezone) and serialised as `"HH:MM"` strings. The applicable clock is the business's own `businesses.timezone`, never the caller's.
- Overnight spans are legal (`opens_at` 22:00 → `closes_at` 02:00); the DB constraint only requires the two to differ. Consumers must handle the wrap.
- A closed day has `is_closed = true` and both times `NULL`.
- Public payloads (`/public/businesses/:slug`, `/marketplace/businesses`) return `hours[]` plus `timezone`. **Open/closed is deliberately not computed server-side** — a cached answer goes stale the moment a business opens or closes, so clients derive it. A business with no rows returns `[]`, never `null`.

## Deployment

- **Prod**: Railway (API + Postgres), built from source with the **Dockerfile
  builder**. That is deliberate: the service ran on Railpack until Aug 2026, which
  skips `scripts/docker-entrypoint.sh` entirely — and since no migration code lives
  in the Go binary, **prod deploys silently applied no migrations**. Keep the
  builder on Dockerfile. Deploys are gated on the `CI` workflow ("Wait for CI").
- **Prod origins**: `CORS_ALLOWED_ORIGINS` / `CLERK_AUTHORIZED_PARTIES` are
  `https://openlocal.store` — the apex is canonical on Vercel and `www` 308s to it.
  Adding a host to the frontend means adding it here too, or Clerk `azp` checks fail.
- **Dev**: private k8s environment on the homelab Talos cluster (manifests in the
  `homelab` repo: `base/openlocal-dev/`). Runs the API + Postgres + frontend
  against the **Clerk dev instance**, reachable only on the LAN via Traefik +
  Pi-hole at `http://api.openlocal-dev.home` (and `http://openlocal-dev.home`).
- **Image**: `.github/workflows/docker.yml` builds the existing `Dockerfile` and
  pushes `ghcr.io/palmieridev/openlocal-api:dev` + `:sha-<sha>` on every push to
  `dev` (GHCR login uses the built-in `GITHUB_TOKEN`; no PAT).
- The image entrypoint runs `golang-migrate` before starting when
  `RUN_MIGRATIONS=true` (single replica in dev keeps this safe). `/healthz` backs
  the k8s readiness/liveness probes.
- Dev env config: `APP_ENV=development` (skips strict prod env validation),
  `CORS_ALLOWED_ORIGINS` / `CLERK_AUTHORIZED_PARTIES` = `http://openlocal-dev.home`.
  Secrets are k8s Secrets created out-of-band, never committed.

## Current Verification

Known passing commands after scaffold:

```sh
sqlc generate
go test ./...
```

The migration acceptance path is:

```sh
make docker-up
migrate -path db/migrations -database "$DATABASE_URL" up
```

Inventory tests live in `internal/inventory/tests`. Their database cases require
`OPENLOCAL_MOVEMENT_TEST_DATABASE_URL` to point to a migrated, disposable local
database whose name starts with `openlocal_movement_test`:

```sh
go test ./internal/inventory/tests
```

Without that variable, the database cases skip. CI provisions this database and
sets both `OPENLOCAL_MOVEMENT_TEST_DATABASE_URL` and
`OPENLOCAL_INTEGRATION_TESTS=1` to exercise inventory and cross-package database
flows explicitly.

Catalog database tests live in `internal/catalog/tests` and require
`OPENLOCAL_CATALOG_TEST_DATABASE_URL` to point to a migrated disposable local
database whose name starts with `openlocal_catalog_test`. CI clones the migrated
test database and enables this suite explicitly.
