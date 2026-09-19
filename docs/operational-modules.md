# Operational modules runbook

Appointments and preorders are persistent business records. Expiration changes a
request's status and appends history; it does not delete the request. PGMQ carries
small routing envelopes for expiration and Stripe reconciliation. A worker retries
temporary failures, moves a job to `openlocal_failed` after eight reads, and performs
recovery sweeps. Archived queue messages are cleaned after seven days. Request
records and their history remain until a future, explicit data-retention policy says
otherwise.

Preorders snapshot names, units, quantities, and prices. They do not reserve or
change inventory. Only the existing stock-movement workflow changes stock levels.

## Local stack

The module stack is isolated from the normal developer database:

```sh
MODULES_TEST_AUTH=true docker compose -f compose.modules.yml up --build
DATABASE_URL='postgres://openlocal:local-modules-only@localhost:15432/openlocal_modules?sslmode=disable' \
  go run ./cmd/modules-fixture -user USER_ID -org ORG_ID
```

The fixture command refuses non-local hosts and any database name other than
`openlocal_modules`. It creates a 30-day Business pilot without contacting Clerk,
Stripe, or WhatsApp.

## Stripe account setup beyond application secrets

1. Activate the Mexican Stripe merchant account, complete business verification,
   and configure the payout bank account and customer-facing business details.
2. Create three active recurring **monthly MXN** prices: Essential (MXN 59),
   Business (MXN 149), and the second-module add-on (MXN 49). Put their price IDs in
   the corresponding environment variables. Startup calls do not create products or
   prices; the API verifies currency, interval, and amount before checkout or changes.
3. Create a Customer Portal configuration. Enable payment-method updates, invoices,
   and cancellation. Keep plan switching disabled because module/resource validation
   is performed by Openlocal before a Stripe change.
4. Register `POST /api/v1/webhooks/stripe` and subscribe to
   `checkout.session.completed`, `customer.subscription.created|updated|deleted`,
   `invoice.paid`, and `invoice.payment_failed`. Webhook delivery only enqueues a
   customer ID; the worker re-fetches Stripe state before changing entitlements.
5. Add the production application URL as the allowed Checkout/Portal return URL.
   Decide tax behavior, invoice branding, cancellation terms, and customer terms
   before going live. Prices in this implementation are tax-exclusive unless the
   Stripe price/account configuration says otherwise.
6. In test mode, forward events with the Stripe CLI and use test clocks/cards to
   exercise renewals, failed payment grace, upgrades, scheduled downgrades, duplicate
   delivery, and cancellation. Do not copy test price or webhook IDs into production.

Production requires PostgreSQL with the PGMQ extension installed before migrations
19 and 20 run, one or more `openlocal-worker` processes, and monitoring of the
`openlocal_failed` queue plus pending `billing_events`.
