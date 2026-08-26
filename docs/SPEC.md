# omarchy-shareware — Platform Spec (v1)

Shareware for the terminal age. Devs ship real Arch packages, users try everything, paying unlocks a signed offline license key. Platform takes **5% of every sale** (and eats Stripe's processing fees out of that side — devs see one number). Stripe Connect (Express, destination charges) moves the money.

Monorepo layout (single Go module `github.com/aphexddb/omarchy-shareware`):

```
license/            # license key format: sign, verify, keygen (pure stdlib crypto/ed25519)
cmd/sharewarectl/   # CLI: keygen, sign, verify (platform + dev tooling)
server/             # HTTP server logic (catalog, buy, webhook, purchase polling, dev onboarding)
cmd/sharewared/     # server entrypoint
client/             # omarket client logic (catalog fetch, buy flow, license store)
cmd/omarket/        # user-facing TUI/CLI
catalog/            # app listings, one JSON file per app (curation via PR)
packaging/          # PKGBUILD template + GitHub Action for devs
examples/           # example paid app wired up end to end
web/                # static landing page served by sharewared at /
docs/               # this spec, DEVELOPERS.md
```

---

## 1. License key format (`SHRW1`)

A license key is a compact, offline-verifiable string:

```
SHRW1.<base64url(payload JSON, no padding)>.<base64url(ed25519 signature, no padding)>
```

- Signature is Ed25519 over the **exact payload bytes** (the JSON as encoded, not re-marshaled).
- Signed by the **platform root key**. Apps embed the platform public key and verify offline.
- No phone-home. No expiry by default. Keys live in `~/.config/shareware/licenses/<app>.key`.

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

`kind` is `"personal"` or `"team"`.

### `license` package public API (exact — other packages compile against this)

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

// EncodeKey / DecodeKey: base64 (std, padded) <-> raw key bytes, for env vars and files.
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

### `sharewarectl` commands

```
sharewarectl keygen                          # prints PUBLIC=... PRIVATE=... (base64)
sharewarectl sign   -key <priv b64> -app <id> [-email x] [-kind personal]   # prints key
sharewarectl verify -pub <pub b64> -license <key or @file>                  # prints payload JSON, exit 1 on bad
```

---

## 2. Catalog

`catalog/*.json`, one app per file, filename `<id>.json`. Curation = pull request.

```json
{
  "id": "hello-shareware",
  "name": "Hello Shareware",
  "description": "One-line-ish description.",
  "version": "1.0.0",
  "homepage": "https://example.com",
  "source": "https://github.com/aphexddb/omarchy-shareware",
  "pkgname": "hello-shareware",
  "price_cents": 900,
  "currency": "usd",
  "stripe_account": "acct_XXX",
  "kind": "source-included",
  "tags": ["demo"]
}
```

- `price_cents: 0` = free (no buy flow).
- `kind`: `"source-included"` (featured tier) or `"closed"`.
- `stripe_account`: the dev's Stripe Connect account id; required when `price_cents > 0`.
- `pkgname`: Arch package name; `omarket install` shells out to `sudo pacman -S <pkgname>` (fall back to `yay -S` if pacman lacks it; on non-Arch, print the command instead).

Server loads the catalog directory at boot (`CATALOG_DIR`, default `./catalog`).

## 3. Server (`sharewared`) HTTP API

Env config:

```
PORT                  default 8484
BASE_URL              e.g. https://market.example.com (used in Stripe redirect URLs)
STRIPE_SECRET_KEY     sk_...
STRIPE_WEBHOOK_SECRET whsec_...
PLATFORM_SIGNING_KEY  base64 ed25519 private key (license.DecodePrivateKey)
CATALOG_DIR           default ./catalog
STATE_PATH            default ./sharewared.db   (bbolt)
WEB_DIR               default ./web             (served at /)
```

Endpoints (JSON errors as `{"error":"..."}` with proper status codes):

- `GET /catalog.json` → `{"apps":[App, ...]}` (the catalog files, as parsed).
- `POST /api/buy` body `{"app":"<id>","email":"<optional>"}` → `{"checkout_url":"https://checkout.stripe.com/...","purchase":"pt_<32 hex>"}`. Creates a Stripe Checkout Session (mode=payment, destination charge):
  - `payment_intent_data.application_fee_amount = price_cents * 5 / 100` (integer floor)
  - `payment_intent_data.transfer_data.destination = app.stripe_account`
  - `success_url = BASE_URL/success?purchase=pt_...`, `cancel_url = BASE_URL/cancel`
  - session metadata: `app`, `purchase`, `email`
  - Store purchase record keyed by token: `{app, email, status:"pending", created_at}`.
- `GET /api/purchase/{token}` → `{"status":"pending"}` or `{"status":"complete","license_key":"SHRW1..."}`. 404 unknown token.
- `POST /stripe/webhook` → verify signature with STRIPE_WEBHOOK_SECRET; on `checkout.session.completed`, look up purchase by metadata, `license.Sign` a key for the app/email, store it, mark complete. Idempotent.
- `POST /api/dev/onboard` body `{"email":"..."}` → `{"account":"acct_...","onboarding_url":"https://connect.stripe.com/..."}`. Creates an Express account + account link (refresh/return to BASE_URL/dev).
- `GET /` and `GET /success`, `/cancel`, `/dev` → static from WEB_DIR (success page tells the buyer to go back to their terminal).
- `GET /healthz` → `{"ok":true}`.

Storage: bbolt, bucket `purchases`, key = token, value = JSON record. No accounts, no sessions.

## 4. Client (`omarket`)

Config dir: `os.UserConfigDir()/shareware/` → `config.json` (`{"server":"https://..."}`, default server `https://omarket.dev` (the canonical instance), overridable with `--server` / `OMARKET_SERVER`), licenses in `licenses/<app>.key`.

Subcommands (stdlib `flag`, no cobra):

```
omarket                      # bubbletea TUI: browse catalog, enter=detail, b=buy, i=install, q=quit
omarket list                 # plain table of catalog
omarket info <app>
omarket install <app>        # pacman/yay shell-out per §2
omarket buy <app> [-email x] # POST /api/buy, print checkout URL + QR (qrterminal),
                             # poll /api/purchase/{token} every 2s (10 min timeout),
                             # save key to licenses/<app>.key, print it big and celebratory
omarket licenses             # list stored keys, verified status
omarket dev onboard -email x # POST /api/dev/onboard, print/open the URL
```

TUI style: keyboard-driven, Tokyo Night-ish palette (lipgloss), no mouse required — it should feel native next to omarchy-menu.

## 5. Money (the whole point)

- Dev lists app with their Stripe Connect account. Buyer pays `price_cents`.
- Platform takes 5% via `application_fee_amount`; Stripe's processing fees come out of the platform's cut (destination charges = platform is merchant of record).
- Dev nets 95%. One number, no tiers, no subscriptions required, no DRM phone-home.
