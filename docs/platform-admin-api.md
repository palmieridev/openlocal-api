# Private platform administration

All routes below require a verified Clerk bearer token and an exact Clerk user ID
in `PLATFORM_ADMIN_USER_IDS`. This environment variable is a comma-separated list;
whitespace and empty entries are ignored. An absent or empty list denies everyone.
Organization roles, active organizations, business memberships, and client metadata
do not grant platform access. An allowlisted user does not need an active org or a
business membership. Configure the list on the API process through the normal
configuration mechanism; no administrator-management endpoint exists.

Authentication fails with `401`. Authenticated users outside the allowlist receive
`403` with `code: "platform_admin_required"`. These checks happen before input
validation, actor upsert, or business/audit lookups. An allowed identity is upserted
into the existing local users table for audit attribution. Platform authorization
does not grant membership or change authorization on ordinary business endpoints.

Admin responses use `Cache-Control: no-store` and the existing private rate limit.
Errors have `{ "error": "message", "code": "optional_machine_code" }`; clients must
handle a missing `code` using the HTTP status. Invalid input returns `400`, an
unknown business returns `404`, and rate limiting returns `429`. An unavailable
database can return `503`; other persistence failures return `500`.

## Search businesses

`GET /api/v1/admin/businesses?q=...&limit=25&cursor=...`

- `q` is optional, trimmed, and at most 200 characters. Empty lists all businesses.
- Name and slug use case-insensitive literal substring matching. `%` and `_` have
  no wildcard meaning. Canonical UUID text and Clerk organization IDs match exactly.
- Includes all business statuses, including drafts and archived businesses.
- `limit` defaults to 25 and must be 1–100. `cursor` is an opaque continuation value.
- Order is `created_at DESC, id DESC`. Keep `q` unchanged between pages. Cursors are
  positions, not authorization credentials; every page rechecks platform access.

Returns `{ "items": [Business], "next_cursor": null, "limit": 25 }`.
`items` is always an array; a null `next_cursor` ends pagination. Each business has
`id`, `name`, `slug`, nullable `clerk_org_id`, `business_type`, `status`, `timezone`,
`created_at`, and `updated_at`. Clerk IDs are private and are not added to public
business DTOs.

## Inspect a business

`GET /api/v1/admin/businesses/:id`

Returns:

```json
{
  "business": {
    "id": "c70c39cc-dc31-4d96-bc43-c9a2c53402df",
    "name": "Example salon",
    "slug": "example-salon",
    "clerk_org_id": "org_example",
    "business_type": "services",
    "status": "active",
    "timezone": "America/Mexico_City",
    "created_at": "2026-11-01T00:00:00Z",
    "updated_at": "2026-11-01T00:00:00Z"
  },
  "subscription": null,
  "access_role": "owner",
  "modules": {
    "appointments": {
      "owned": false,
      "enabled": false,
      "can_accept_new": false,
      "can_manage_existing": true,
      "can_configure": false,
      "can_operate": true,
      "can_export": false,
      "reason": "module_unavailable",
      "usage": 0,
      "limit": 0,
      "max_resources": 0,
      "advanced": false
    }
  }
}
```

`modules` always contains `appointments` and `preorders` (pickup orders). The example
shows one entry for brevity. Capabilities and usage come from the existing
entitlement service evaluated as a business owner, indicated by `access_role`.
These flags describe business-owner capabilities; they do not authorize the admin
to act through normal business routes. Enabled configuration is distinct from
subscription ownership. Usage counts confirmed records for the subscription's
exact stored billing-period start, rather than counting every appointment/order.

When present, `subscription` contains `tier`, `status`, `modules`, nullable
`stripe_customer_id`, nullable `stripe_subscription_id`, nullable `period_start`,
nullable `period_end`, nullable `grace_until`, `cancel_at_period_end`, nullable
`scheduled_change`, and `updated_at`. It reflects the stored subscription. An absent
subscription returns null and free capabilities with zero usage; inspection does
not create a subscription or enable a module. No customer names, phones, request
notes, or module settings are included.

## Read activity

`GET /api/v1/admin/activity?business_id=...&actor_user_id=...&action=...&limit=25&cursor=...`

Reads the existing audit log, including business and platform mutations. Optional
`business_id` and `actor_user_id` filters are local UUIDs; `action` is an exact,
trimmed string of at most 120 characters. All supplied filters apply together.
Pagination has the same limit and ordering rules as business search; keep filters
unchanged between pages. Business and activity cursors are not interchangeable.

Returns `{ "items": [Activity], "next_cursor": null, "limit": 25 }`. Each activity
has `id`, nullable `business_id`, nullable local `actor_user_id`, `action`,
`entity_type`, nullable `entity_id`, JSON `metadata`, and `created_at`. Null foreign
keys can represent deleted businesses/users. Metadata is private and may contain
internal reasons and before/after state. These records must not be displayed in
public or business-owner views without a separate projection.

The mutation infrastructure writes metadata to the same `audit_logs` table through
caller-supplied sqlc queries, so a transaction rollback also rolls back its audit.
Existing audit callers continue writing empty metadata. This API does not create
audit records for search, detail, or activity reads.
