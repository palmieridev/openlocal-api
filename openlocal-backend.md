# Openlocal API Backend Plan

## Product

Openlocal is an open-source commerce hub for local sellers: inventory, storefronts, barcode stock control, and map-based product discovery.

MVP scope:

- Clerk-backed business owner login and organization membership
- one Clerk organization per business
- business profiles and public storefronts
- product catalog, variants, images metadata, categories, tags
- barcodes, generated internal SKUs/codes, and manual lookup
- inventory movement ledger and stock levels
- public marketplace search and map bounding-box filters
- ABC, Pareto, EOQ, and low-stock analytics
- CSV import/export later in the MVP sequence

Explicitly excluded from MVP:

- CFDI/SAT integration
- payments
- delivery logistics
- multi-branch inventory
- native mobile app
- federation/self-hosted sync

## Architecture

Stack:

- Go
- Fiber
- PostgreSQL
- sqlc
- golang-migrate
- Docker Compose
- Clerk JWT auth via JWKS
- OpenAPI documentation in `docs/openapi.yaml`

Structure:

```text
cmd/api
internal/auth
internal/business
internal/catalog
internal/inventory
internal/marketplace
internal/analytics
internal/audit
internal/config
internal/server
internal/middleware
internal/platform/postgres
internal/platform/logger
internal/platform/validator
db/migrations
db/queries
docs
scripts
```

Pattern:

```text
handler -> service/domain rule -> repository/sqlc
```

Handlers parse, validate, authorize, call domain behavior, and map DTOs. They must not return raw sqlc structs from public endpoints.

## Auth And Tenancy

Clerk owns:

- identities
- sessions
- passwords/MFA
- organization membership

Openlocal stores only:

- local user projection keyed by `clerk_user_id`
- businesses
- business memberships keyed by `business_id`, local `user_id`, and `clerk_org_id`
- role projection: `owner`, `manager`, `staff`

Private routes require:

1. valid Clerk bearer token
2. active Clerk organization claim where business data is accessed
3. matching local `business_members` row for that Clerk org
4. allowed Openlocal role

No local password, refresh-token, or session tables are part of the design.

## Data Protection

Public DTOs can expose:

- public business profile
- public location fields
- public products
- price/currency
- `public_stock_status`

Public DTOs must not expose:

- cost
- supplier/private notes
- exact private stock
- movement history
- internal user contact data beyond public business contact fields
- Clerk IDs

## Current Milestones

1. Foundation: implemented.
2. External setup: Clerk app linked, `.env.local` pulled, GitHub remote attached.
3. Auth: Clerk JWT middleware and local user sync implemented.
4. Business profiles: create/get/update and public profile endpoints implemented.
5. Catalog: product and variant CRUD implemented.
6. Inventory: movement ledger and stock-level transaction implemented.
7. Marketplace: public businesses/products/search implemented.
8. Analytics: ABC/Pareto, EOQ, low-stock endpoints implemented.
9. Hardening/docs: README, OpenAPI starter, AGENTS.md, tests, security defaults implemented.

