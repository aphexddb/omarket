# omarket public API (v1)

The HTTP calls the `omarket` CLI makes to `https://omarket.dev` (or any
compatible server, via `-server` / `OMARKET_SERVER`). Two surfaces:

- **Buyer API** — unauthenticated. Browse the catalog, start a purchase,
  poll it, fetch the license-signing public keys.
- **Seller API** — authenticated with a bearer token. Create a seller
  account, claim and edit app listings, set up payouts, mint test licenses.

Money, once: the buyer pays `price_cents` through Stripe Checkout. The
platform is the merchant of record, keeps a flat 5% (Stripe fees come out
of that), and Stripe transfers the remaining 95% to the developer's
Connect account.

Out of scope here: the admin/curation API (`/api/admin/*`, platform-operator
only), CLI behavior, and the Go `license` package apps compile against for
offline key verification (see [license/license.go](../license/license.go)).

## Conventions

- Requests and responses are JSON. Request bodies that would otherwise be
  empty are sent as `{}`.
- Errors are `{"error": "message"}` with a 4xx/5xx status.
- **Auth**: seller endpoints (marked below) require
  `Authorization: Bearer <seller_token>`. Buyer endpoints take no auth.
- **Long-poll**: endpoints marked `?wait=N` park the request up to `N`
  seconds (server-clamped to `[0, 25]`) until the awaited state change or
  the timeout, whichever is first. A server that predates long-polling
  ignores `?wait=` and answers instantly.
- **Rate limiting**: a `429` with `{"error":"slow_down"}` and a
  `Retry-After` header (seconds) can answer any polling endpoint. It is not
  terminal — clients sleep `max(Retry-After, interval)` and retry. The
  `interval` values some responses carry (seconds between polls) are
  server-authoritative cadence hints; absent, clients fall back to their own
  backoff.

---

## Buyer API

### `GET /api/catalog.json` — the catalog

```
200 {"apps": [App, ...]}
```

Each `App`:

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

- `price_cents: 0` — free; no checkout, but `POST /api/buy` still works
  (see the free path below).
- `ware` — the "-ware" tradition the listing follows (`"shareware"`,
  `"beerware"`, `"charityware"`, ...). Free-form, max 64 chars, defaults
  to `"shareware"`.
- `comment` — the one-line ask that goes with the `ware`. 3–140 chars.
- `author` — author handle, typically a GitHub username, stored bare
  (`aphexddb`, no `@`). Max 64 chars.
- `pkgname` — Arch package name, for installers.
- `stripe_account` — the developer's Stripe Connect account id. Present
  when `price_cents > 0`.

Caching: responses carry `ETag: "<hex sha256 of the body>"` and
`Cache-Control: public, max-age=300`. A request with
`If-None-Match: <etag>` (quoted or bare) gets `304 Not Modified` with an
empty body when the catalog hasn't changed.

Legacy: `GET /catalog.json` (the pre-v0.2 path) answers
`301 Moved Permanently` to `/api/catalog.json`; same body either way.

### `POST /api/buy` — start a purchase

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
- `callback_port` / `callback_nonce` — optional, required together
  (loopback callback, below). Port `1024–65535`; nonce 8–64 chars of
  `[A-Za-z0-9_-]`.

```
200 {"checkout_url", "purchase", "free", "ware", "comment", "author",
     "interval", "expires_in"}
```

- **Priced app**: `checkout_url` is a Stripe Checkout link for the buyer
  to open; `purchase` is an opaque token to poll while they pay.
- **Free app**: `free: true`, no `checkout_url`, and the `purchase` token
  is already complete — the first poll returns the license. `ware`,
  `comment`, and `author` come back so the client can show what the app
  asks of the person at the moment they acquire it.
- `interval` — seconds between polls (cadence hint, see Conventions).
- `expires_in` — seconds the token remains worth polling/reconciling.

**Loopback callback**: when `callback_port`/`callback_nonce` are present,
the server builds the Stripe `success_url` as
`{server}/success?purchase={token}&cb_port={port}&cb_nonce={nonce}` — it
never accepts a URL, host, or path from the client, only the validated
port and nonce. After payment, the success page top-level-navigates to
`http://127.0.0.1:{port}/done?cb_nonce={nonce}`, waking the client's
listener early. The callback is a hint to poll now, never a delivery
channel — it never carries the license key, and completion is only ever
confirmed by `GET /api/purchase/{token}`.

### `GET /api/purchase/{token}` — poll a purchase (`?wait=N`)

```
200 {"status": "pending", "interval": 5}          // still waiting
200 {"status": "complete", "license_key": "SHRW1..."}
404 {"error": "..."}                              // unknown token
```

- `interval` on a pending body is an optional mid-wait cadence refresh.
- With `?wait=N`, the request parks until the purchase completes or `N`
  seconds pass (see Conventions).

### `GET /api/pubkey` — license-signing keys

```
200 {"public_key", "key_id", "fingerprint",
     "keys": [{"key_id", "algorithm", "public_key", "fingerprint"}, ...]}
```

- `public_key` — standard base64 of the raw Ed25519 public key.
- `key_id` — `pk_` + first 12 lowercase hex chars of
  `sha256(raw public key bytes)`.
- `fingerprint` — `SHA256:<full lowercase hex digest>`.
- `keys` lists every active signing key; verifiers accept the first entry
  that verifies. The top-level fields mirror `keys[0]` for older clients;
  a server that predates `keys` sends only the top-level fields.

---

## Seller API

All endpoints except `POST /api/sellers` require
`Authorization: Bearer <seller_token>`.

Sellers see apps as `AppPublic` — a different shape from the catalog `App`
above:

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

App id rule: `^[a-z0-9-]{3,64}$`, no leading or trailing hyphen, and the
server enforces a reserved-names list. Field limits match the catalog:
`ware` optional, max 64 chars, defaults to `"shareware"`; `comment`
required, 3–140 chars; `author` required, max 64 chars.

### `POST /api/sellers` — create a seller account

No auth. Body `{}`.

```
201 {"seller_id", "seller_token", "onboarding_url"}
```

`seller_token` authenticates every other seller call — store it.
`onboarding_url` is always `""`: this endpoint never touches Stripe
(payouts setup is `POST /api/sellers/payouts`).

### `GET /api/sellers/me` — seller status (`?wait=N`)

```
200 {"seller_id", "charges_enabled", "onboarding_url",
     "apps": [AppPublic, ...]}
```

- `charges_enabled` — whether Stripe will accept charges for this seller.
  Served from a server-side cache kept current by a Stripe webhook, not a
  live Stripe call per request.
- `onboarding_url` — `""` until the seller has started payouts setup;
  otherwise a plain request mints a fresh Stripe onboarding link. A
  `?wait=N` request **never** mints one (it comes back `""` even
  mid-onboarding) — minting is a Stripe call the long-poll path exists to
  avoid. `?wait=N` parks the request for a `charges_enabled` change.

### `POST /api/sellers/payouts` — start Stripe onboarding

Body `{}`.

```
200 {"stripe_account", "onboarding_url"}
503 {"error": "..."}    // server has no Stripe configured
```

Creates the seller's Stripe Connect account if needed. If one already
exists, the same `200` shape carries a fresh onboarding link — or
`onboarding_url: ""` once charges are enabled (treat as "already set up").

### `POST /api/apps` — claim an app id

```json
{"id": "my-app-name"}
```

```
201 AppPublic
400 {"error": "..."}    // invalid id
409 {"error": "..."}    // taken or reserved
```

### `PUT /api/apps/{id}` — update a listing (owner only)

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
400 {"error": "..."}    // invalid field
```

### `POST /api/apps/{id}/test-license` — mint a test key (owner only)

```
200 {"license_key": "SHRW1..."}
```

The license has `kind: "test"`.

---

## Appendix: license keys (`SHRW1`)

The `license_key` strings above are offline-verifiable:

```
SHRW1.<base64url(payload JSON, no padding)>.<base64url(ed25519 signature, no padding)>
```

The signature is Ed25519 over the exact payload bytes as encoded (not
re-marshaled), made with a key published by `GET /api/pubkey`. Payload:

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

`kind` is `"personal"`, `"team"`, or `"test"`. Apps embed the platform
public key and verify offline — no phone-home, no expiry by default.

## Notes

- A soft-deleted app (an admin action) disappears from every read path
  above: the catalog, `POST /api/buy`, and the seller's own
  `GET /api/sellers/me`.
- The catalog's `listed` flag is seller/admin-facing only; it is served in
  `AppPublic`, never in `/api/catalog.json`.
