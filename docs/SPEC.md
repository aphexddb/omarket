# omarket — platform spec (v1)

The contract between the `omarket` client, a `sharewared` server, and apps
that check licenses. Three primitives: a catalog the client fetches, a
purchase that ends in a signed key file on the buyer's disk, and a seller
API behind `omarket sell`.

Money, once: the buyer pays `price_cents` through Stripe Checkout. The
platform is the merchant of record, keeps a flat 5%, and pays Stripe's
processing fees out of that 5%. Stripe transfers the remaining 95% to the
developer's Connect account directly.

## 1. License key format (`SHRW1`)

A license key is an offline-verifiable string:

```
SHRW1.<base64url(payload JSON, no padding)>.<base64url(ed25519 signature, no padding)>
```

- The signature is Ed25519 over the **exact payload bytes** — the JSON as
  encoded, not re-marshaled.
- Signed by the platform root key. Apps embed the platform public key and
  verify offline. No phone-home, no expiry by default.
- Keys are stored at `~/.config/shareware/licenses/<app>.key`
  (`os.UserConfigDir()/shareware/licenses` precisely).

Payload JSON fields:

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

`kind` is `"personal"`, `"team"`, or `"test"` (test licenses come from
`omarket sell testkey`, §4).

### `license` package public API (apps compile against this)

```go
package license

type License struct {
    V           int    `json:"v"`
    ID          string `json:"id"`
    App         string `json:"app"`
    EmailSHA256 string `json:"email_sha256"`
    IssuedAt    int64  `json:"issued_at"`
    Kind        string `json:"kind"`
}

var ErrInvalidFormat error  // key doesn't parse as SHRW1.x.y
var ErrBadSignature error   // signature check failed

// GenerateKeypair returns a new ed25519 keypair.
func GenerateKeypair() (pub ed25519.PublicKey, priv ed25519.PrivateKey, err error)

// Encode/Decode: base64 (std, padded) <-> raw key bytes, for env vars and files.
func EncodePrivateKey(priv ed25519.PrivateKey) string
func DecodePrivateKey(s string) (ed25519.PrivateKey, error)
func EncodePublicKey(pub ed25519.PublicKey) string
func DecodePublicKey(s string) (ed25519.PublicKey, error)

// NewLicense fills id (lic_ + 16 hex crypto/rand), issued_at (now), hashes email.
func NewLicense(app, email, kind string) License

// HashEmail returns hex sha256 of strings.ToLower(strings.TrimSpace(email)); "" for "".
func HashEmail(email string) string

// Sign serializes l to JSON and returns the SHRW1 key string.
func Sign(l License, priv ed25519.PrivateKey) (string, error)

// Verify parses and checks the signature; returns the payload on success.
func Verify(key string, pub ed25519.PublicKey) (*License, error)
```

Keypair generation and license signing are platform-operator tooling,
outside this repo. Everyone else verifies: `omarket verify` (§3) for
humans, `license.Verify` for apps.

## 2. Catalog

`catalog/*.json` in the server repo, one app per file, filename `<id>.json`.
Curation is a pull request. The server loads the directory at boot
(`CATALOG_DIR`, default `./catalog`) and serves it at `GET /api/catalog.json`.

`GET /catalog.json` is the former path and now answers `301 Moved
Permanently` with `Location: /api/catalog.json`. Clients through v0.1.0
request the old path and follow the redirect; from v0.2 the client requests
`/api/catalog.json` directly. The body and shape are the same either way.
Not to be confused with `GET /api/catalog` (§4), a different endpoint in a
different shape.

`GET /api/catalog.json` also answers `ETag: "<hex sha256 of the body>"` and
`Cache-Control: public, max-age=300`. A client that sends
`If-None-Match: <etag>` (quoted or bare) gets `304 Not Modified` with an
empty body when the catalog hasn't changed since; otherwise a normal `200`
with a fresh `ETag`. The `omarket` client keeps a disk cache
(`~/.config/shareware/cache/`) keyed by server URL: a cache under 5 minutes
old is used with no request at all, an older one is revalidated with
`If-None-Match`, and a network error serves the stale cache with a note
rather than failing outright. A server or client that predates this is
unaffected — `If-None-Match` absent means an unconditional `200`, exactly as
before.

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
  "tags": ["demo"],
  "listed": false
}
```

- `price_cents: 0` — free; no buy flow.
- `ware`: the "-ware" tradition the listing follows — `"shareware"`,
  `"beerware"`, `"coffeeware"`, `"charityware"`, and so on. Free-form, not
  an enum; max 64 chars, defaults to `"shareware"` when empty.
- `comment`: required, 3–140 chars. The one-line ask that goes with the
  `ware`, e.g. `"Buy me a beer if you like this tool. Cheers!"`.
- `author`: required, max 64 chars. Author handle, typically a GitHub
  username, stored bare (`aphexddb`, no `@`).
- `stripe_account`: the dev's Stripe Connect account id. Required when
  `price_cents > 0`.
- `pkgname`: Arch package name. `omarket install` prefers `omarchy pkg add
  <pkgname>` (Arch + the Omarchy package repo, with Omarchy's polkit GUI
  for sudo), else `pkexec pacman -S --noconfirm --needed`, else `yay -S`.
  On systems with none of those it prints the command instead of running it.
- `listed`: optional, seed-file only — it is never served in
  `/api/catalog.json`, and clients never see it. It states whether the app
  belongs in the browse catalog when the server first seeds it; absent, a
  reserved id seeds unlisted and everything else seeds listed. Either way an
  app priced below the platform minimum seeds unlisted. Seeding only ever
  creates rows the server doesn't have yet, so this decides nothing about an
  app that already exists. The platform repo owns the details.

## 3. Client (`omarket`)

Default server: `https://omarket.dev`. Override with `-server` or
`OMARKET_SERVER`. Licenses are read and written under
`~/.config/shareware/licenses/<app>.key` (§1). Every command that reads the
catalog — the TUI, `buy` with no app id, `list`, `info`, `install` — fetches
`GET /api/catalog.json` (§2).

Five top-level commands: `buy`, `sell`, `licenses`, `verify`, `version`.

```
omarket                      # TUI: browse catalog, enter=detail, b=buy, i=install, q=quit
omarket buy                  # plain table of the catalog (no app id given)
omarket buy <app> [-email x] # POST /api/buy, print checkout URL + QR (qrterminal),
                             # wait for the purchase to complete (layered wait,
                             # below; 10 min live budget), save the key to
                             # licenses/<app>.key, print the path
omarket licenses             # list stored keys with verified status
omarket verify <key|path|-> [-server <url>]
                             # verify a SHRW1 key: the arg is the key itself,
                             # a path to a key file, or "-" for stdin. Offline
                             # by default: SHAREWARE_PUBLIC_KEY if set, else
                             # the baked-in platform key. -server instead
                             # fetches that server's GET /api/pubkey and
                             # verifies against its key(s).
omarket version              # print the version
```

### Waiting for a purchase

`omarket buy` no longer polls a fixed endpoint on a fixed clock. It layers
three mechanisms, strictly decreasing in optimism — each upper one is purely
an optimization; only the bottom one is a correctness guarantee:

1. **Loopback callback.** `POST /api/buy` may include `callback_port`
   (1024–65535) and `callback_nonce` (8–64 chars of `[A-Za-z0-9_-]`; required
   together). When present, the server builds the Stripe `success_url` as
   `{baseURL}/success?purchase={token}&cb_port={port}&cb_nonce={nonce}`
   instead of its plain form — never accepting a URL/host/path from the
   client, only the validated port and nonce. The success page then
   top-level-navigates to `http://127.0.0.1:{port}/done?cb_nonce={nonce}`.
   The CLI's listener answers only `GET /done`, compares the nonce in
   constant time, and on a match closes a tiny wake-up page and treats that
   as a *hint* to check now — it never carries the license key, and a wrong
   or missing nonce just gets a bodyless 404 with the listener left running.
   Unavailable over SSH, headless CI, or when the port can't bind; failing
   to bind is silent and simply omits both fields from the request.
2. **Long-poll.** `GET /api/purchase/{token}?wait=N` (`N` seconds, server-
   clamped to `[0, 25]`) parks the request until the purchase completes or
   `N` seconds pass, whichever is first, answering
   `{"status":"pending","interval":N}` on timeout (`interval` optional, a
   mid-wait cadence refresh) or the usual complete body the moment it's
   ready. An old server ignores `?wait=` and answers instantly — safe
   because of the client-side floor below.
3. **Durable pending record + reconcile.** The moment `POST /api/buy`
   returns a token — before the checkout URL is even printed — the CLI
   writes `~/.config/shareware/pending/<token>.json`
   (`{"token","app","server","created_at","expires_at"}`, 0600). If the live
   wait ends without completing (timeout, Ctrl-C, crash, sleep), the record
   stays. `omarket licenses` and the TUI reconcile pending records on every
   launch: one `GET /api/purchase/{token}` per record (oldest first, up to
   5 per run) against its own recorded server — complete lands the license,
   an unknown token (404) or an expired record (past `expires_at` plus a 1h
   clock-skew grace) drops it with a one-line notice, anything else is left
   for next time.

Cadence between requests is server-authoritative: `POST /api/buy` returns
`interval` (seconds between polls/long-poll re-issues) and `expires_in`
(seconds the token is worth reconciling, governing the pending record's
`expires_at` — independent of the 10-minute live-wait budget). Without a
server-sent `interval` (an old server), the client decays its own poll gap:
2s, ×1.5 per empty response, capped at 15s, ±20% jitter, never below the 2s
floor — this floor is what makes an ignored `?wait=` harmless. A `429` with
`Retry-After` (`{"error":"slow_down"}`) is not terminal: the client sleeps
`max(Retry-After, interval)` and keeps waiting.

`omarket list`, `omarket info <app>`, and `omarket install <app>` still work
but are no longer advertised in `omarket -h`: `list` is `buy` with no
arguments, and `info`/`install` are reachable from the TUI. `list` carries a
WARE column; `info` shows `ware`, `author`, and the `comment`.

`GET /api/pubkey` -> `200 {"public_key","key_id","fingerprint","keys":[{"key_id","algorithm","public_key","fingerprint"}]}`.

- `public_key`: standard base64.
- `key_id`: `pk_` + first 12 lowercase hex chars of `sha256(raw public key bytes)`.
- `fingerprint`: `SHA256:<full lowercase hex digest>`.
- The top-level fields mirror `keys[0]`, for older clients. `omarket verify
  -server` walks `keys[]` and accepts the first entry that verifies; against
  a server that predates `keys[]` it falls back to the top-level
  `public_key`.

## 4. Selling API

The seller-facing API behind `omarket sell`, separate from the
catalog.json/buy flow in §2/§3. `AppPublic` is what a seller edits, not
what a buyer browses; it is distinct from the catalog `App` shape in §2.

```
GET  /api/catalog                          -> 200 {"platform_fee_percent": int, "apps": [AppPublic...]}  (listed apps only)
GET  /api/apps/{id}                        -> 200 AppPublic (listed or not), 404 unknown
POST /api/sellers                          (no auth, empty JSON body)
                                            -> 201 {"seller_id","seller_token","onboarding_url"}
                                            (onboarding_url is always "" — this
                                            endpoint never touches Stripe)
GET  /api/sellers/me[?wait=N]               (Authorization: Bearer <seller_token>)
                                            -> 200 {"seller_id","charges_enabled","onboarding_url","apps":[AppPublic...]}
                                            (onboarding_url is "" until the
                                            seller has started payouts setup;
                                            `charges_enabled` is served from a
                                            server-side cache kept current by
                                            a Stripe webhook, not a live
                                            Stripe call on every request —
                                            see below. `?wait=N` (seconds,
                                            same [0,25] clamp as purchase
                                            long-poll) parks the request for
                                            a status change; a `?wait=`
                                            response never mints a fresh
                                            `onboarding_url` — it comes back
                                            "" even mid-onboarding, since
                                            minting one is a Stripe call this
                                            path exists to avoid. A plain
                                            (no `?wait=`) request still mints
                                            one as before. Same 429
                                            `slow_down`/`Retry-After` as
                                            purchase polling can apply here.)
POST /api/sellers/payouts                  (Bearer, empty JSON body)
                                            -> 200 {"stripe_account","onboarding_url"};
                                            if a Stripe account already exists,
                                            same 200 shape with a fresh
                                            onboarding link, or onboarding_url:""
                                            once charges are enabled; 503
                                            {"error"} if the server has no
                                            Stripe configured
POST /api/apps                             (Bearer) {"id": "my-app-name"}
                                            -> 201 AppPublic; 409 if taken/reserved; 400 invalid
PUT  /api/apps/{id}                        (Bearer, owner) {"name","description","homepage","price_usd_cents","ware","comment","author"}
                                            -> 200 AppPublic; 400 invalid field
                                            ("comment" and "author" are
                                            required; "ware" defaults to
                                            "shareware")
POST /api/apps/{id}/test-license           (Bearer, owner) -> 200 {"license_key": "SHRW1..."} (license kind "test")
```

`AppPublic = {"id","name","description","homepage","price_usd_cents","listed","ware","comment","author"}`.

Field limits, shared with §2: `ware` max 64 chars (optional), `comment`
3–140 chars (required), `author` max 64 chars (required).

App id rule: `^[a-z0-9-]{3,64}$`, no leading or trailing hyphen. The server
also enforces a reserved-names list.

Error responses: `{"error": "message"}`, matching §3's convention.

Curation — setting an app's `listed` flag, publishing platform-owned
listings, soft-deleting an app, minting a license server-side — is done by
the platform operator against `/api/admin/*` with private tooling, gated on
a server-side admin token. None of it is exposed in this CLI, so it is out
of scope for this spec; the platform repo documents it. One effect is
visible here: a soft-deleted app is filtered out of every read path above,
including `GET /api/catalog`, `GET /api/apps/{id}`, `POST /api/buy`, and
`GET /api/sellers/me`.

The `omarket.json` manifest `omarket sell claim` writes, and `omarket sell
push` reads:

```json
{
  "id": "my-app-name",
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
omarket sell init            # POST /api/sellers (or GET /api/sellers/me if
                             # already initialized); saves seller_token.
                             # No Stripe involved.
omarket sell claim <app-id>  # POST /api/apps; writes a template
                             # omarket.json manifest to the cwd. Prints the
                             # ware suggestion list and pre-fills author
                             # from git config (github.user, then
                             # user.email)
omarket sell push            # reads ./omarket.json; PUT /api/apps/{id}.
                             # Refuses to push while template placeholder
                             # values remain. If the pushed price is > 0 and
                             # payouts aren't set up, prints a hint to run
                             # `omarket sell payouts` (never auto-opens a
                             # browser)
omarket sell testkey [app]   # POST /api/apps/{id}/test-license; verifies the
                             # key locally, then saves it
omarket sell payouts         # POST /api/sellers/payouts; opens the returned
                             # onboarding_url in the browser and exits —
                             # fire-and-exit, no polling. Onboarding is a
                             # human filling in bank forms, sometimes over
                             # days, so the CLI doesn't wait for it; re-check
                             # with `omarket sell status` whenever. If
                             # already enabled, says so and exits. 503 means
                             # the server has no Stripe configured.
omarket sell status [-wait]  # GET /api/sellers/me. -wait long-polls
                             # (?wait=25 per request) for up to 2 minutes or
                             # until charges_enabled flips, then prints
                             # status either way — useful right after
                             # finishing the browser onboarding flow.
```
