# omarchy-shareware — Platform Spec (v1)

Shareware for the terminal age. Devs ship real packages, users try everything, paying unlocks a signed offline license key.

How it works:
- Dev lists an app (ex: using Stripe Connect), buyer pays `price_cents`
- Platform takes 5%. Stripe's processing fees, risk radar, etc. come out of the platform (the merchant of record). Stripe pays the developer directly
- Dev nets 95%. No subscriptions required, no DRM.

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

### `license` package public API (packages compile against this)

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

## 2. Catalog

`catalog/*.json`, one app per file, filename `<id>.json`. Curation = pull request.

```json
{
  "id": "hello-shareware",
  "name": "Hello Shareware",
  "description": "One-line-ish description.",
  "version": "1.0.0",
  "homepage": "https://example.com",
  "source": "https://github.com/aphexddb/omarket",
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

## 3. Client (`omarket`)

Default server `https://omarket.dev` (the canonical instance). This is overridable with `--server` or setting `OMARKET_SERVER`. All licenses are stored in `licenses/<app>.key`.

```
omarket                      # main TUI: browse catalog, enter=detail, b=buy, i=install, q=quit
omarket list                 # plain table of catalog
omarket info <app>
omarket install <app>        # pacman/yay shell-out
omarket buy <app> [-email x] # POST /api/buy, print checkout URL + QR (qrterminal),
                             # poll /api/purchase/{token} every 2s (10 min timeout),
                             # save key to licenses/<app>.key, print it big and celebratory
omarket licenses             # list stored keys, verified status
```

## 4. Selling API

A second, seller-facing API — separate from the catalog.json/buy flow in
§2/§3 above — backs `omarket sell`. `AppPublic` here is distinct from the
catalog `App` shape in §2: it's what a seller edits, not what a buyer
browses.

```
GET  /api/catalog                          -> 200 {"platform_fee_percent": int, "apps": [AppPublic...]}  (listed apps only)
GET  /api/apps/{id}                        -> 200 AppPublic (listed or not), 404 unknown
POST /api/sellers                          (no auth, empty JSON body)
                                            -> 201 {"seller_id","seller_token","onboarding_url"}
GET  /api/sellers/me                       (Authorization: Bearer <seller_token>)
                                            -> 200 {"seller_id","charges_enabled","onboarding_url","apps":[AppPublic...]}
POST /api/apps                             (Bearer) {"id": "my-app-name"}
                                            -> 201 AppPublic; 409 if taken/reserved; 400 invalid
PUT  /api/apps/{id}                        (Bearer, owner) {"name","description","homepage","price_usd_cents"}
                                            -> 200 AppPublic
POST /api/apps/{id}/test-license           (Bearer, owner) -> 200 {"license_key": "SHRW1..."} (license kind "test")
```

`AppPublic = {"id","name","description","homepage","price_usd_cents","listed"}`.

App id rule: `^[a-z0-9-]{3,64}$`, no leading/trailing hyphen. The server also
enforces a reserved-names list.

Error responses: `{"error": "message"}`, matching §3's convention.

Listing/curation (setting an app's `listed` flag) is performed by the
platform operator with private tooling, not exposed in this CLI.

```
omarket sell init            # POST /api/sellers (or GET /api/sellers/me if
                             # already initialized); saves seller_token;
                             # opens onboarding_url; polls /api/sellers/me
                             # until charges_enabled (~5 min timeout, not
                             # an error — reprints the URL to finish later)
omarket sell claim <app-id>  # POST /api/apps; generates a template
                             # omarket.json manifest in the cwd
omarket sell push            # reads ./omarket.json; PUT /api/apps/{id};
                             # refuses to push while template placeholder
                             # values remain
omarket sell testkey [app]   # POST /api/apps/{id}/test-license; saves and
                             # locally verifies the key
omarket sell status          # GET /api/sellers/me
```
