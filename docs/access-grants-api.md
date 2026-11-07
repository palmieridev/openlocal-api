# Complimentary operational access

These private platform-admin endpoints use the authorization and error contract in
[platform-admin-api.md](platform-admin-api.md). An ordinary organization admin is
denied. All responses use `Cache-Control: no-store`. IDs are nonzero UUIDs. No
endpoint enables intake, changes a subscription, bills/cancels anything, applies a
checkout discount, changes stock, or resets usage.

## Create and list grants

`POST /api/v1/admin/businesses/:id/grants`

```json
{
  "module": "appointments",
  "tier": "business",
  "starts_at": "2026-11-10T00:00:00Z",
  "expires_at": "2027-01-10T00:00:00Z",
  "indefinite": false,
  "internal_reason": "Approved launch assistance",
  "owner_message": "Appointments are complimentary through January 9."
}
```

- `module`: exactly `appointments` or `preorders` (pickup orders).
- `tier`: exactly `essential` or `business`.
- `starts_at`: required RFC3339 timestamp with offset; may be now, past, or scheduled.
- `expires_at`: optional RFC3339 timestamp, strictly after start and in the future.
  To omit expiry, explicitly send `indefinite: true` and omit/null `expires_at`.
  A timestamp and `indefinite: true` together are invalid. Timestamp years must be
  2000–9999; persistence/comparisons use UTC with microsecond precision.
- `internal_reason`: required, trimmed, 1–2000 characters; platform-admin only.
- `owner_message`: optional/null; when supplied, trimmed, 1–2000 characters.
- Unknown properties and invalid JSON shape are rejected.

Returns `201` with a private `Grant`: `id`, `business_id`, `module`, `tier`,
`starts_at`, nullable `expires_at`, `indefinite`, computed `status`,
`internal_reason`, nullable `owner_message`, nullable local `created_by`,
`created_at`, `updated_at`, positive `version`, nullable `revoked_at`, nullable
local `revoked_by`, and nullable `revocation_reason`. Deleted actors become null;
immutable audit snapshots retain their original IDs. Status is `revoked` first,
then `scheduled` before start, `expired` at/after expiry, otherwise `active`.
Start is inclusive; expiry is exclusive. Overlapping grants are allowed.

`GET /api/v1/admin/businesses/:id/grants?limit=25&offset=0`

Returns `{ "items": [Grant], "total": 1, "limit": 25, "offset": 0 }`, including
scheduled, expired, and revoked grants. Order is `created_at DESC, id DESC`.
`limit` is 1–100; `offset` is 0–2000000000. Arrays are never null.

## Extend and revoke

`POST /api/v1/admin/businesses/:id/grants/:grant_id/extend`

```json
{
  "version": 1,
  "expires_at": "2027-03-10T00:00:00Z",
  "indefinite": false,
  "internal_reason": "Extend launch assistance"
}
```

Expiry must move strictly forward into the future. An expired grant can be
extended, restoring its contribution immediately. A scheduled grant keeps its
start. Send `indefinite: true` with omitted/null `expires_at` to extend a finite
grant indefinitely. Already indefinite or revoked grants cannot be extended.
Module, level, start, original reason, and owner message are immutable; issue a
new grant and revoke the old one to replace them. Each mutation requires a new
internal reason. The owner message is not rewritten automatically on extension.

`POST /api/v1/admin/businesses/:id/grants/:grant_id/revoke`

```json
{ "version": 2, "internal_reason": "Assistance concluded" }
```

Revocation is immediate and permanent. Both routes return `200` with the updated
`Grant` and incremented `version`. Stale versions, an already revoked grant, or
extension of an indefinite grant return `409` / `version_conflict`. Invalid
input returns `400`; missing businesses or a grant outside the business returns
`404`. Repeated revocation returns `409`; clients can list grants to refresh.

Grant edits and confirmations serialize on the same subscription row lock. A
confirmation ordered after a committed revoke cannot use that grant. Grant,
reason, actor, and before/after audit insertion commit atomically; audit failure
rolls back the mutation.

## History

`GET /api/v1/admin/businesses/:id/grants/:grant_id/history?limit=25&offset=0`

Returns `{ "items": [History], "limit": 25, "offset": 0 }` with the same bounds.
Each history item has `id`, nullable local `actor_user_id`, `action`, `at`, and
`metadata: { "internal_reason": "...", "before": Grant|null, "after": Grant }`.
Actions are `access_grant.created`, `access_grant.extended`, and
`access_grant.revoked`. History is ordered by resulting grant version ascending;
timestamps reflect audit insertion transaction time. Expiry is computed, so it
has no mutation entry. The same records appear in `/api/v1/admin/activity`.
Internal history is never included in owner entitlement responses.

## Effective entitlements and subscription reads

`GET /api/v1/businesses/:id/entitlements` returns `tier` (stored subscription
tier), local `role`, `period_start`, `period_end`, and `modules` keyed by
`appointments` and `preorders`. Each module adds authoritative effective `tier`
and `sources` to the existing access flags, usage, limit, and resource fields:

```json
{
  "tier": "business",
  "sources": [{
    "kind": "grant",
    "tier": "business",
    "grant_id": "c70c39cc-dc31-4d96-bc43-c9a2c53402df",
    "starts_at": "2026-11-10T00:00:00Z",
    "expires_at": null,
    "owner_message": "Complimentary appointments."
  }]
}
```

Only active sources appear. `kind` is `subscription`, `pilot`, or `grant`;
`grant_id` and `owner_message` are optional. `expires_at` is nullable (indefinite).
Sources expose no internal reasons, grant editors, or audit metadata. Clients
must use each module's effective tier, since the two modules can differ.
`GET /api/v1/businesses/:id/subscription` retains stored billing fields and
embeds these effective `entitlements`. The private platform business detail also
projects effective module tiers and sources.

For each module, the strongest active paid/pilot/grant capability wins. Essential
allows 50 confirmations and 1 appointment resource; Business allows 1000 and 5.
Quotas and resources are never added across sources. Revocation/expiry removes
only that source; other paid or grant access continues. Existing pilot and
past-due grace access continue. Grant changes never alter the usage window.

While paid/pilot/grace access is active, usage keeps the existing exact stored
subscription-period-start accounting. Otherwise, the window is the UTC calendar
month `[first day 00:00, next first day 00:00)` and counts confirmations by event
time, including confirmations originally assigned to a different period marker.
Creating, extending, replacing, revoking, or overlapping grants cannot reset it.
Confirmation uses a transaction lock, so concurrent requests cannot exceed the
strongest level's single limit. Intake remains controlled by module settings.

Owners retain record/history reads and existing terminal/notes/final-quantity
operations after access expires or is revoked. Confirmation, rescheduling, new
intake, and configuration require current effective access. Managers/staff need
Business for that module; managers configure/export, staff cannot configure or
export. A Business grant that has started retains team history/record operation
rights after expiry/revocation, matching expired Business subscription behavior;
a grant revoked before its scheduled start does not. Creating/confirming,
configuration, advanced settings, resource limits, and exports always require
the current module capability. Customer history omits modules the actor cannot
manage. Existing records, request tokens, settings, and usage events are retained.

When a Business source ends and Essential remains, saved resources and advanced
configuration are retained. Intake uses only the first active appointment
resource in `name, id` order (the first five at Business). Saved resource-specific
hours/assignment, service buffers for new appointments, and preorder cutoff/slot
capacity apply only at Business. Existing appointment duration/buffer snapshots
are preserved when managing records. Restoring Business restores the saved
configuration. A paid downgrade also respects an active Business appointment
grant before deactivating extra resources; provider requests/pricing are unchanged.
