# Openlocal API

Openlocal API is a Go/Fiber/PostgreSQL backend for local commerce: business profiles, storefront catalogs, inventory movements, stock levels, marketplace discovery, and inventory analytics.

## Stack

- Go + Fiber
- PostgreSQL with `pgx/v5`
- `sqlc` for typed queries
- `golang-migrate` migrations
- Clerk authentication, with one Clerk organization per business
- Docker Compose for local Postgres
- PGMQ for durable expiration and billing reconciliation jobs
- Direct Stripe Billing in MXN; Clerk remains authentication and organization identity

## Local Setup

```sh
make db-up
migrate -path db/migrations -database "$DATABASE_URL" up
make seed
sqlc generate
go test ./...
go run ./cmd/api
```

For the complete local stack, including PostgreSQL with PGMQ, migrations, the
API hot-reload process, and the background worker, run:

```sh
docker compose up --build
```

The migration service finishes before the API starts. Stripe configuration is
optional locally; without it, module operations still work and only paid
checkout and plan changes are unavailable.

Local Stripe sandbox values can be kept separately in the gitignored
`.env.stripe.local` file. Compose loads it when present for the API and worker,
so Clerk and database settings can remain in their existing environment files.

### Subscription discount codes

New module subscriptions enable promotion-code entry in Stripe Checkout.
Create a coupon and its customer-facing promotion code in the Stripe Dashboard
for the same sandbox or live account used by the API. Stripe validates redemption
restrictions, expiration, and limits and displays the discounted total before
payment. The frontend's plan prices remain the monthly amounts before discounts.

Codes are redeemed only when starting a subscription, not through the existing
subscription preview/change endpoints. Matching open checkouts created without
promotion-code support are expired and replaced; checkouts that already support
codes are reused. Existing subscriptions still use the change flow or portal.
Deploy this API change alongside the web branch `feat/subscription-discount-codes`
to make the code-entry option available.

## Production migrations

Railway builds the API from `main` using the Dockerfile. Its entrypoint runs
`migrate up` against `DATABASE_URL` before starting the API. Migration execution
defaults to enabled (`RUN_MIGRATIONS=true`); a failure prevents the new API process
from starting. Pending numbered `.up.sql` files are applied in order, while a
database already at the latest version returns `no change` and starts normally.

Keep Dockerfile builds enabled and leave the API start-command override unset.
Commit each schema change as a new numbered migration pair in `db/migrations`;
never edit an already released migration to introduce a new schema change.
CI tests upgrades from version 18 using both the PGMQ extension on Postgres 17
and SQL-only PGMQ on Postgres 18. CI databases are temporary, not production.

Before merging a release, back up production and satisfy the prerequisites in
[`docs/operational-modules.md`](docs/operational-modules.md). API startup owns
migrations; workers use `RUN_MIGRATIONS=false` and start only after the API's
migrations succeed. A code rollback does not roll back database migrations.
If a migration fails, inspect the database version/dirty state and repair the
failure before retrying; do not blindly force a version or run a down migration.

## Security Defaults

- Clerk bearer tokens are verified against issuer, JWKS, expiry, and configured authorized parties.
- Business authorization requires both the active Clerk organization role and the matching local membership role.
- Production startup rejects test-auth bypass, wildcard CORS, missing authorized parties, and a missing Clerk webhook secret.
- Requests use strict JSON decoding, a 1 MiB body limit, bounded pagination, domain validation, and parameterized sqlc queries.
- Global, public, private, and webhook rate limits are configurable through `.env.example`. The built-in limiter is per process; use a shared edge or Redis-backed limiter before running multiple API replicas.
- Stock movement writes require an `Idempotency-Key` header and update the movement ledger and stock level in one transaction.

## Business location and category tables

- `businesses.location_mode` distinguishes fixed, mobile, and hybrid operations.
- `business_service_areas` stores structured public coverage for mobile and hybrid businesses. It is used by marketplace reach filtering and is replaced atomically with the business profile.
- `business_categories` is reserved for business-level taxonomy and is not a geographic table. It is currently unused.
- `categories` contains each business's product catalogue categories; the marketplace seed populates this table, which is why it may have rows while `business_categories` does not.

## Tests

- Keep pure validation and mapping tests beside their domain package (`internal/catalog`, `internal/inventory`, and similar).
- Keep middleware and HTTP security behavior tests in `internal/server` and `internal/auth`.
- Keep database transaction and tenant-isolation tests in `internal/integration`; they are opt-in and roll back their fixtures.

```sh
go test ./...
go test -race ./...
OPENLOCAL_INTEGRATION_TESTS=1 go test ./internal/integration
```

The appointment/preorder API contract is documented in
[`docs/modules-api.md`](docs/modules-api.md). Deployment, queue retention, local
fixtures, and the non-secret Stripe setup are in
[`docs/operational-modules.md`](docs/operational-modules.md).

## Delivery workflow and manual images

Develop on feature branches and open pull requests directly into `main`. CI runs
for pull requests targeting `main` and pushes to `main`. Railway production keeps
its existing Dockerfile build and Wait for CI gate. There is no long-lived `dev`
branch and no automatic GHCR image publishing.

Use the localhost Compose stack above for development with the Clerk development
instance. Cluster workloads are optional; build an image locally when needed:

```sh
docker build --platform linux/amd64 -t openlocal-api:local .
```

For a cluster that pulls images from GHCR, authenticate to the registry, then tag
and push the local image manually:

```sh
docker tag openlocal-api:local ghcr.io/palmieridev/openlocal-api:dev
docker push ghcr.io/palmieridev/openlocal-api:dev
```

Build for the cluster's CPU architecture and supply environment configuration and
secrets at runtime. The image still includes the API, worker, and migration
entrypoint; API startup applies migrations, while workers use
`RUN_MIGRATIONS=false` as described above.
