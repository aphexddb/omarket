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
omarket dev onboard -email x # POST /api/dev/onboard, print/open the URL
```
