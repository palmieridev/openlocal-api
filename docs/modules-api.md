# Operational modules API

All routes use `/api/v1`. Owner requests require Clerk authentication and an active organization matching the business. Monetary and quantity values are decimal strings. Timestamps are ISO 8601 with offsets. Errors preserve `error` and add a stable `code`. Mutating request/billing operations require `Idempotency-Key`.

## Settings and configuration

`GET/PATCH /businesses/:id/:module/settings`, where module is `appointments` or `preorders`, returns the complete settings object:

```json
{"enabled":false,"hours":[],"closed_dates":[],"lead_minutes":60,"horizon_days":30,"slot_minutes":30,"cancel_before_minutes":60,"pending_ttl_minutes":1440,"daily_cutoff":"","max_orders_per_slot":0,"variant_rules":[]}
```

Hours use `{day_of_week:0,opens_at:"09:00",closes_at:"18:00",is_closed:false}`, Monday=0, overnight intervals allowed. `closed_dates` contains local `YYYY-MM-DD` dates. Empty hours means intake is not configured. Variant rules use `{variant_id,min_quantity:"0.001",max_quantity:"100",step:"0.001"}` and explicitly enable that variant for preorder. PATCH accepts partial settings and validates the resulting configuration. Advanced capacity/cutoff settings require Business. `PATCH /businesses/:id/modules/:module` accepts `{enabled:true}`.

`GET/POST /businesses/:id/services`, `PATCH /businesses/:id/services/:service_id`: service `{id,name,description,duration_minutes,price:string|null,is_public,active,buffer_minutes}`. POST omits id; price can be null. Buffer minutes require Business.

`GET/POST /businesses/:id/appointment-resources`, `PATCH /businesses/:id/appointment-resources/:resource_id`: resource `{id,name,active,service_ids:string[],hours:Hour[],closed_dates:string[]}`. Empty service_ids permits all services; empty resource hours uses module hours. List endpoints return arrays. Essential permits one active resource and no resource-specific schedule. Archiving preserves historical records.

## Public requests

`GET /public/businesses/:slug/appointments/services` returns service array.

`GET /public/businesses/:slug/appointments/availability?service_id=UUID&date=YYYY-MM-DD&resource_id=UUID` returns `{timezone,slots:[{resource_id,starts_at,ends_at}]}`. Resource filter optional.

`GET /public/businesses/:slug/preorders/catalog` returns `{timezone,items:[{variant_id,product_id,name,unit,unit_price,currency,min_quantity,max_quantity,step}]}`.

`GET /public/businesses/:slug/preorders/pickup-windows?date=YYYY-MM-DD` returns `{timezone,windows:[{id,starts_at,ends_at}]}`.

POST to `/public/businesses/:slug/appointments` accepts `{customer_name,customer_phone,notes?,service_id,resource_id,starts_at,challenge_token?,locale?}`. POST to the corresponding `/preorders` accepts `{customer_name,customer_phone,notes?,pickup_window_id,items:[{variant_id,quantity,unit_price}],challenge_token?,locale?}`. Public challenge bypass is permitted only in explicitly configured local development.

Creation returns HTTP 201 and a record (below) plus `management_token`. BFF stores the token in an httpOnly cookie; optional recovery links carry it only in the fragment. Public GET `/public/requests/:public_id` and POST `/public/requests/:public_id/cancel` require `X-Openlocal-Management-Token`. Cancellation accepts `{version}`. Invalid credentials return 404. Create retries return the same credential.

```typescript
interface RequestRecord {
  id: string; public_id: string; reference: string;
  module: 'appointments' | 'preorders'; status: string; version: number;
  customer_id: string; customer_name: string; customer_phone: string; notes: string;
  starts_at: string; ends_at: string; expires_at: string; created_at: string;
  timezone: string; service_id?: string; resource_id?: string; service_name?: string;
  duration_minutes?: number; price?: string | null;
  pickup_window_id?: string;
  items?: {id:string;variant_id:string;name:string;unit:string;quantity:string;unit_price:string;total:string;final_quantity:string|null}[];
  estimated_total?: string; final_total?: string|null;
  history: {from_status:string;to_status:string;at:string;actor:string}[];
  business: {name:string;slug:string;whatsapp:string|null};
  allowed_transitions: string[];
}
```

## Owner operations

`GET/POST /businesses/:id/appointments` and `/preorders`: list or owner-create with the same create body minus challenge. Lists accept `limit` (25 default, 100 max), `offset`, optional `status`, `date` (business-local), `resource_id`, `customer_id` and return `{items:RequestRecord[],total,limit,offset}`. `GET /businesses/:id/:module/:record_id` returns a record.

`POST /businesses/:id/:module/:record_id/transitions` accepts `{status,version}`. `PATCH` record accepts `{version,starts_at?,resource_id?,pickup_window_id?,notes?,items?:[{id,final_quantity}],final_total?}`. Time/resource edits are appointment-only; window/items/final_total are preorder-only. Return updated record. Final states reject edits. Quota is consumed once at first confirmation; stale versions and capacity conflicts return 409.

Business reports: `GET /businesses/:id/:module/export?from=YYYY-MM-DD&to=YYYY-MM-DD` returns CSV; `GET /businesses/:id/preorders/summary?date=YYYY-MM-DD` returns `{date,items:[{variant_id,name,unit,quantity}],orders,total_estimated}`; `GET /businesses/:id/module-customers/:customer_id/history` returns `{appointments:RequestRecord[],preorders:RequestRecord[]}` (bounded to latest 100 each).

## Entitlements and subscription

`GET /public/plans` returns `{currency:"MXN",second_module_price:"49.00",billing_available:boolean,plans:[{id:"free"|"essential"|"business",monthly_price:string,confirmation_limit:number,max_resources:number,team_access:boolean,advanced:boolean}]}`. Price values are configurable on the server.

`GET /businesses/:id/entitlements` returns `{tier,role,period_start,period_end,modules:{appointments:ModuleAccess,preorders:ModuleAccess}}` where `ModuleAccess` is `{owned,enabled,can_accept_new,can_manage_existing,can_configure,can_operate,can_export,reason,usage,limit,max_resources,advanced}`.

Owner `GET /businesses/:id/subscription` returns `{tier,status,modules:string[],period_start,period_end,cancel_at_period_end,grace_until:string|null,scheduled_change:object|null,billing_available:boolean,entitlements:Entitlements}`. Free/pilot subscriptions also use this shape.

`POST /businesses/:id/billing/checkout` accepts `{tier:"essential"|"business",modules:["appointments"],locale?:"es"|"en"}` and returns `{url}`. `POST .../portal` accepts `{locale?}` and returns `{url}`. `POST .../preview` and `/change` accept `{tier,modules,retained_resource_id?:string,locale?,proration_date?:number}`. Preview returns `{amount_due:string,currency:"MXN",effective_at:string,timing:"immediate"|"period_end",proration_date:number}`. Change returns `{pending:boolean,timing,effective_at}`. Checkout/changes grant no entitlements until verified Stripe state permits them.

Typical error codes: `invalid_request`, `not_found`, `module_unavailable`, `quota_exceeded`, `slot_unavailable`, `pickup_window_full`, `version_conflict`, `price_changed`, `request_expired`, `invalid_transition`, `billing_unavailable`, `rate_limited`, `challenge_failed`. Presentation should use localized codes, not raw backend messages. Nonmodule existing endpoints keep their current shape.
