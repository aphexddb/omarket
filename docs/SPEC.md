# omarket public API (v1)

Base URL: `https://omarket.dev`.

## Conventions

- Requests and responses are JSON. Otherwise-empty request bodies are `{}`.
- Errors: `{"error": "message"}` with a 4xx/5xx status.
- Buyer endpoints: no auth. Seller endpoints:
  `Authorization: Bearer <seller_token>`.
- `?wait=N`: server holds the request up to `N` seconds (clamped to
  `[0, 25]`) until the awaited state change or the timeout. Servers without
  long-poll support ignore `?wait=` and answer instantly.
- Polling endpoints may answer `429` `{"error":"slow_down"}` with a
  `Retry-After` header (seconds). Not terminal: sleep
  `max(Retry-After, interval)` and retry.
- `interval` response fields: seconds between polls, server-authoritative.

## Buyer API

### GET /api/catalog.json

```
200 {"apps": [App, ...]}
```

`App`:

```json
{
  "id": "hello-shareware",
  "name": "Hello Shareware",
  "description": "What the app does, in one line.",
  "version": "1.0.0",
  "homepage": "https://example.com",
  "source": "https://github.com/aphexddb/omarket",
  "pkgname": "hello-shareware",
  "price_cents": 900,
  "currency": "usd",
  "stripe_account": "acct_XXX",
  "ware": "shareware",
  "comment": "Try it free. Buy a key if you keep it around.",
  "author": "aphexddb",
  "tags": ["demo"]
}
```

- `price_cents: 0` — free listing.
- `ware` — free-form, max 64 chars, defaults `"shareware"`.
- `comment` — required, 3–140 chars.
- `author` — required, max 64 chars, bare handle (no `@`).
- `pkgname` — Arch package name.
- `stripe_account` — present when `price_cents > 0`.

Headers: `ETag: "<hex sha256 of body>"`,
`Cache-Control: public, max-age=300`. Request header
`If-None-Match: <etag>` (quoted or bare) → `304` with empty body when
unchanged.

`GET /catalog.json` → `301` `Location: /api/catalog.json`.

### POST /api/buy

```json
{
  "app": "hello-shareware",
  "email": "buyer@example.com",
  "callback_port": 49321,
  "callback_nonce": "u_0aF3-xQ"
}
```

- `app` — required.
- `email` — optional; hashed into the license.
- `callback_port`, `callback_nonce` — optional, required together.
  Port `1024–65535`; nonce 8–64 chars of `[A-Za-z0-9_-]`.

```
200 {"checkout_url", "purchase", "free", "ware", "comment", "author",
     "interval", "expires_in"}
```

- Priced app: `checkout_url` (Stripe Checkout link) + `purchase` (opaque
  poll token).
- Free app: `free: true`, no `checkout_url`, `purchase` token already
  complete. `ware`, `comment`, `author` populated.
- `interval` — poll cadence, seconds.
- `expires_in` — seconds the token remains pollable.

```
409 {"error": "..."}    seller has no payouts set up, or the id is claimed
                        but never published (no PUT /api/apps/{id} yet)
```

Callback: when `callback_port`/`callback_nonce` are set, the Stripe
`success_url` is
`{server}/success?purchase={token}&cb_port={port}&cb_nonce={nonce}`. The
server accepts only the port and nonce, never a client-supplied URL, host,
or path. The success page top-level-navigates to
`http://127.0.0.1:{port}/done?cb_nonce={nonce}`. The callback never
carries the license key; completion is confirmed only by
`GET /api/purchase/{token}`.

### GET /api/purchase/{token}  (`?wait=N`)

```
200 {"status": "pending", "interval": 5}
200 {"status": "complete", "license_key": "SHRW1..."}
404 {"error": "..."}
```

- `interval` on a pending body is optional (mid-wait cadence refresh).
- `?wait=N` holds until complete or timeout.

### GET /api/pubkey

```
200 {"public_key", "key_id", "fingerprint",
     "keys": [{"key_id", "algorithm", "public_key", "fingerprint"}, ...]}
```

- `public_key` — standard base64 of the raw Ed25519 public key.
- `key_id` — `pk_` + first 12 lowercase hex chars of
  `sha256(raw public key bytes)`.
- `fingerprint` — `SHA256:<full lowercase hex digest>`.
- `keys` — all active signing keys; verifiers accept the first entry that
  verifies. Top-level fields mirror `keys[0]`. Older servers send only the
  top-level fields.

## Seller API

All endpoints except `POST /api/sellers` require
`Authorization: Bearer <seller_token>`.

`AppPublic`:

```json
{
  "id": "my-app-name",
  "name": "My App Name",
  "description": "One line about what your app does",
  "homepage": "https://example.com",
  "price_usd_cents": 500,
  "listed": true,
  "ware": "shareware",
  "comment": "What you ask of people who use it",
  "author": "aphexddb"
}
```

- App id: `^[a-z0-9-]{3,64}$`, no leading or trailing hyphen, reserved
  names rejected.
- `ware` — optional, max 64 chars, defaults `"shareware"`.
- `comment` — required, 3–140 chars.
- `author` — required, max 64 chars.

### POST /api/sellers

No auth. Body `{}`.

```
201 {"seller_id", "seller_token", "onboarding_url"}
```

- `seller_token` — authenticates all other seller calls.
- `onboarding_url` — always `""` on this endpoint.

### GET /api/sellers/me  (`?wait=N`)

```
200 {"seller_id", "charges_enabled", "onboarding_url",
     "apps": [AppPublic, ...]}
```

- `charges_enabled` — whether Stripe accepts charges for this seller.
  Served from a webhook-fed cache, not a live Stripe call.
- `onboarding_url` — `""` until payouts setup has started; a plain request
  mints a fresh onboarding link. A `?wait=N` request never mints one and
  always returns `""`.
- `?wait=N` holds for a `charges_enabled` change.

### GET /api/sellers/stats

```
200 {"seller_id",
     "apps": [{"id", "name", "price_usd_cents", "ware", "listed",
               "licenses", "gross_usd_cents"}, ...],
     "total_licenses", "total_gross_usd_cents"}
```

- One row per live app the seller owns, sorted by id, including apps that
  have sold nothing.
- `licenses` — completed purchases only; an abandoned checkout is not a
  sale. Free acquisitions count.
- `gross_usd_cents` — `price_usd_cents * licenses` at the app's *current*
  price. An estimate, before the platform fee and Stripe's cut.
- Never calls Stripe.

### POST /api/sellers/payouts

Body `{}`.

```
200 {"stripe_account", "onboarding_url"}
503 {"error": "..."}
```

- Creates the Stripe Connect account if needed; otherwise returns a fresh
  onboarding link.
- `onboarding_url: ""` once charges are enabled.
- `503` — server has no Stripe configured.

### POST /api/apps

```json
{"id": "my-app-name"}
```

```
201 AppPublic
400 {"error": "..."}    invalid id
409 {"error": "..."}    taken or reserved
```

### PUT /api/apps/{id}

Owner only.

```json
{
  "name": "My App Name",
  "description": "One line about what your app does",
  "homepage": "https://example.com",
  "price_usd_cents": 500,
  "ware": "shareware",
  "comment": "What you ask of people who use it",
  "author": "aphexddb"
}
```

```
200 AppPublic
400 {"error": "..."}    invalid field
```

- `price_usd_cents` — either `0` or at least `100` ($1.00). `1`-`99` is
  rejected. `0` is a ware-only listing: no payment, no Stripe account
  required, and buyers see the `ware` and `comment` instead of a checkout
  page.

### POST /api/apps/{id}/test-license

Owner only.

```
200 {"license_key": "SHRW1..."}
```

License `kind` is `"test"`.

## License keys (SHRW1)

```
SHRW1.<base64url(payload JSON, no padding)>.<base64url(ed25519 signature, no padding)>
```

- Signature: Ed25519 over the exact payload bytes as encoded (not
  re-marshaled), by a key published at `GET /api/pubkey`.
- Verification is offline; no expiry by default.

Payload:

```json
{
  "v": 1,
  "id": "lic_<16 hex random>",
  "app": "hello-shareware",
  "email_sha256": "<hex sha256 of lowercased trimmed email, or empty>",
  "issued_at": 1756000000,
  "kind": "personal"
}
```

- `kind` — `"personal"` (paid), `"ware"` (free acquisition of a ware-only
  listing), `"team"`, or `"test"`.

## Notes

- A soft-deleted app disappears from every read path above: the catalog,
  `POST /api/buy`, and `GET /api/sellers/me`.
- `listed` is served in `AppPublic` only, never in `/api/catalog.json`.
