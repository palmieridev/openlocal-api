# Personal account API contract

All routes below are relative to `/api/v1`. Send `Authorization: Bearer <Clerk session token>`. No active organization or business membership is required; owners and staff have the same personal capabilities. Responses use `Cache-Control: no-store`. The API resolves ownership from the verified token subject; no request body accepts a user identity. Phone/name are contact data only.

## Saved businesses

| Method | Path | Success |
| --- | --- | --- |
| GET | `/me/saved-businesses` | 200 cursor page |
| GET | `/me/saved-businesses/{business_id}` | 200 `{"saved":true}` or `{"saved":false}` |
| PUT | `/me/saved-businesses/{business_id}` | 200 `{"saved":true}`; empty body |
| DELETE | `/me/saved-businesses/{business_id}` | 204; also when absent |

PUT is idempotent and preserves the original save time. Only active businesses can be saved (otherwise 404). State lookup returns false for an unsaved UUID even if that business does not exist. Saved entries contain `business_id`, `saved_at` (RFC3339), `available`, `name`, `slug`, `logo_url`. The last three are nullable; all are null when the business is unavailable. Archiving retains a removable entry; hard deletion removes it.

## Customer requests

| Method | Path | Success |
| --- | --- | --- |
| GET | `/me/activity` | 200 cursor page of summaries |
| GET | `/me/activity/{module}/{request_id}` | 200 detail |
| POST | `/me/businesses/{business_id}/appointments` | 201 detail |
| POST | `/me/businesses/{business_id}/preorders` | 201 detail |
| POST | `/me/activity/{module}/{request_id}/cancel` | 200 detail |

`module` is `appointments` or `preorders`. Activity accepts an optional `module` query filter. Each entry represents one request, sorted newest created first, with its current status. Disabled modules, subscription lapse, and business archival do not gate history/detail reads. New requests require an active business and pass existing intake, subscription, availability, quota and abuse checks. Cancellation follows existing status and cutoff rules even if intake is disabled.

Creation bodies use the existing guest input shape. Both accept `customer_name` (2–120 characters), international `customer_phone`, optional `notes` (max 2000), `challenge_token` (Turnstile action `request`, required in production), and optional `locale`. Appointment also requires `service_id`, `resource_id`, `starts_at` (RFC3339 timestamp with offset). Preorder also requires `pickup_window_id` and `items` (1–50 unique variants), each with `variant_id`, decimal-string `quantity`, and decimal-string `unit_price` matching current catalog price. Unknown JSON properties are rejected. Cancellation body is `{"version":1}` using the latest detail version.

Creation and cancellation require `Idempotency-Key`: 8–128 characters from letters, numbers, dot, underscore, colon, dash. Reuse the same key and body when retrying; replay lasts 24 hours, returns the original status/body and `Idempotent-Replayed: true`. A different body with the same key returns 409 `version_conflict`. Challenge tokens are excluded from the body digest. Personal replay scopes include the authenticated person's local ID, business, action and module (and request ID for cancellation). Ownership is checked before cancellation replay.

Summary shape:

```json
{
  "id": "uuid", "module": "appointments", "reference": "ABCDEF12",
  "status": "requested", "created_at": "2026-11-03T12:00:00Z",
  "starts_at": "2026-11-04T16:00:00Z", "ends_at": "2026-11-04T16:30:00Z",
  "timezone": "America/Mexico_City",
  "business": {"id":"uuid", "name":"Local", "slug":"local", "available":true, "logo_url":null}
}
```

Unavailable businesses retain their name for personal history; `slug` becomes null. Do not offer a storefront link then. Detail adds `version`, `expires_at`, `customer_name`, `customer_phone`, `history[]` (`from_status`, `to_status`, `at`, `actor`) and `allowed_transitions[]` (empty or `["cancelled"]`). Appointments add `service_id`, `service_name`, `duration_minutes` and optional `price`. Preorders add `items[]` (`id`, `variant_id`, `name`, `unit`, `quantity`, `unit_price`, `total`, nullable `final_quantity`), `estimated_total`, and optional `final_total`. Amounts and quantities are strings. Totals describe requests, not payment evidence. Module-specific fields are omitted when inapplicable.

Personal DTOs omit management tokens, internal customer IDs, resource IDs and notes (staff can edit existing notes). Guest creation/detail/cancellation remain on their existing public routes with management-token authorization. Signed-in creation must use the personal endpoints to appear in account activity. Sending a token to guest creation does not associate it with an account. There is no guest claiming or phone/email backfill. Account deletion removes saves and clears request ownership while retaining business records; existing guest access retains its own expiration rules.

## Pagination and errors

Both lists accept `limit` (default 25, 1–100) and `cursor` (opaque, URL-encode it). Response is `{"items":[],"next_cursor":null,"limit":25}`. Continue with `next_cursor` until null, preserving the activity module filter. Cursors are scoped to the person, list and filter. Stable descending ties use ID and, for activity, module. Changing the filter requires starting a new page. No total count is returned.

Errors follow `{"error":"message","code":"optional_machine_code"}`. Some existing framework errors omit `code`; use status as fallback and localize in the web client.

- 400: invalid UUID, JSON, limit, cursor, module, or idempotency key; domain validation uses `invalid_request`; challenge failure uses `challenge_failed`.
- 401: missing/invalid bearer token.
- 403 `module_unavailable`: new request intake unavailable.
- 404: missing or unowned request, or unavailable business on save/create. Other users' requests are indistinguishable from missing requests.
- 409: `version_conflict`, `request_expired`, `invalid_transition`, `slot_unavailable`, `pickup_window_full`, `price_changed`, `rate_limited` as applicable.
- 429: middleware rate limit; retry later.
- 503: challenge/credential configuration unavailable; `challenge_failed` or `module_unavailable` as applicable.

## Web follow-up (PR #29)

Add Save/Unsave controls; preserve locale and a validated local return path/intended save through sign-in. Forward session bearer tokens through the BFF, use these DTOs/types, route signed-in appointment/preorder creation to personal endpoints, and link activity to authenticated detail pages rather than guest-token pages. Add localized loading/error/empty states, unavailable-business handling and versioned cancellation. Preserve guest BFF/token flows. No web files are changed in this API branch.
