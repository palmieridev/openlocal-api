# Clerk Webhooks

Configure one Clerk webhook endpoint per deployed API environment.

Local development with a tunnel:

```text
POST https://<your-tunnel-host>/api/v1/webhooks/clerk
```

Production:

```text
POST https://<api-domain>/api/v1/webhooks/clerk
```

Subscribe to these Clerk events:

- `user.created`
- `user.updated`
- `organization.created`
- `organization.updated`
- `organization.deleted`
- `organizationMembership.created`
- `organizationMembership.updated`
- `organizationMembership.deleted`

Set this environment variable from the Clerk webhook endpoint settings:

```env
CLERK_WEBHOOK_SIGNING_SECRET=whsec_...
```

## Local Tunnel

Start the API locally:

```sh
go run ./cmd/api
```

Then expose port `8080` with one of these:

```sh
ngrok http 8080
```

or:

```sh
cloudflared tunnel --url http://localhost:8080
```

Use the HTTPS forwarding URL from the tunnel as the Clerk webhook endpoint:

```text
https://<forwarding-host>/api/v1/webhooks/clerk
```

Tunnel URLs change unless you reserve a domain. If the URL changes, update the Clerk webhook endpoint.

## Signature Verification

Clerk webhooks are delivered through Svix. Every request includes:

- `svix-id`
- `svix-timestamp`
- `svix-signature`

The API verifies the request by:

1. Removing the `whsec_` prefix from `CLERK_WEBHOOK_SIGNING_SECRET`.
2. Base64-decoding the remaining secret.
3. Building the signed payload as `svix-id.svix-timestamp.raw_body`.
4. Computing an HMAC-SHA256 digest.
5. Comparing that digest to every `v1` signature in `svix-signature` using constant-time comparison.
6. Rejecting timestamps outside a five-minute tolerance.

Never process webhook JSON before signature verification. The raw request body is what is signed.

## Event Handling

Handled events:

- `user.created`, `user.updated`: upsert the local `users` projection.
- `user.deleted`: remove local user PII while preserving the row for audit/history references.
- `organization.deleted`: archive matching Openlocal businesses.
- `organizationMembership.created`, `organizationMembership.updated`: upsert local business membership when the Clerk organization maps to a local business.
- `organizationMembership.deleted`: remove the local membership.
- `organization.created`, `organization.updated`: acknowledged as no-op because the local business is created by the onboarding API.

Webhook delivery is eventually consistent and can arrive out of order. Events that reference a Clerk organization with no matching local business are acknowledged and ignored.
