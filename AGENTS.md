# AGENTS.md

omarket sells DRM-free shareware licenses. A license is a signed string in a
file. Apps verify it offline — no account, no activation server, no runtime
network call, ever.

Format and API contract: [`docs/SPEC.md`](docs/SPEC.md). Working reference
implementations in four languages: [`examples/`](examples).

## The CLI

```bash
go install github.com/aphexddb/omarket/cmd/omarket@latest
```

Buying:

```bash
omarket buy <app>            # Stripe URL + QR, waits, writes the key file
omarket licenses             # list stored keys, verified status
omarket verify <key|path|->  # re-verify offline, any time
```

Selling — run in this order, once per app:

```bash
omarket sell                 # status if a seller token exists, otherwise help
omarket sell init            # creates a seller account, saves a token
omarket sell claim <app-id>  # claims the id, writes ./omarket.json
# edit omarket.json: name, description, price_usd_cents, ware, comment, author
omarket sell push            # publishes it; refuses template placeholders
omarket sell testkey         # mints a kind:"test" license so you can test
omarket sell payouts         # Stripe onboarding, when you want the money
omarket sell status          # seller account + claimed apps
```

App id must match `^[a-z0-9-]{3,64}$`. Some names, including `omarket`, are
reserved. `comment` (3–140 chars) and `author` are required; `ware` defaults
to `shareware`. Default server is `https://omarket.dev`; override with
`-server` or `OMARKET_SERVER`.

## Adding a license check to an app

A key is `SHRW1.<base64url(payload JSON)>.<base64url(ed25519 sig)>`, both
segments unpadded. `omarket buy` writes it to
`$XDG_CONFIG_HOME/shareware/licenses/<app-id>.key` (`~/.config/...` when unset),
mode 0600.

Six steps, in this order:

1. Read the key file, trim whitespace.
2. Split on `.` — three parts, first is `SHRW1`.
3. Base64url-decode parts 2 and 3. The signature is exactly 64 bytes.
4. **Ed25519-verify over the decoded payload bytes**, against the baked-in
   platform public key.
5. Parse the JSON, require `v == 1`.
6. **Require `app` to equal your app id.**

Go — import the package and you get 1–5 for free:

```go
import "github.com/aphexddb/omarket/license"

const (
    appID = "your-app-id"
    // Platform signing key, also at https://omarket.dev/api/pubkey.
    platformPublicKey = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko="
)

func registered() bool {
    dir, err := os.UserConfigDir()
    if err != nil {
        return false
    }
    raw, err := os.ReadFile(filepath.Join(dir, "shareware", "licenses", appID+".key"))
    if err != nil {
        return false
    }
    pub, err := license.DecodePublicKey(platformPublicKey)
    if err != nil {
        return false
    }
    lic, err := license.Verify(strings.TrimSpace(string(raw)), pub)
    return err == nil && lic.App == appID // step 6 is yours to make
}
```

Other languages: copy [`examples/c`](examples/c) (libcrypto),
[`examples/rust`](examples/rust) (ed25519-dalek), or
[`examples/ruby`](examples/ruby) (stdlib openssl). Change `APP_ID` and go.
Run any of them against [`examples/testdata`](examples/testdata) to see a
valid, a tampered, and a wrong-app key handled.

## Rules

- **Verify over the decoded bytes, never a re-encoding.** Parse-then-
  re-serialize has to reproduce field order and spacing exactly; a JSON
  library upgrade breaks it. Decode → verify → parse.
- **Check the app id.** A SHRW1 key for someone else's app carries a valid
  platform signature. Skip step 6 and any omarket license unlocks your app.
- **Bake the public key into the binary.** Do not fetch `/api/pubkey` at
  runtime — that reintroduces the phone-home the format exists to avoid. It
  is a public key; shipping it is the design.
- **`SHAREWARE_PUBLIC_KEY` overrides it**, for tests and local stacks. Honour
  it; do not invent another mechanism.
- **No expiry, no revocation, no seat counting.** `issued_at` is provenance,
  not a deadline. Do not add time checks.
- **`kind` is `personal`, `ware`, `team`, or `test`.** Treat `ware` (a free,
  ware-only listing) and `test` as registered; say so in the UI if it matters.
- **`email_sha256` is a hash.** The platform never gives your binary a buyer's
  address. Do not try to reverse it.
- **Unregistered is not an error.** Nag and keep running. That is shareware.

## Working in this repo

```bash
make build     # go build -o omarket ./cmd/omarket
make test      # go test ./...
make examples  # C, Go, Rust, Ruby against examples/testdata
```

`license/` is the key format, `client/` is the CLI's guts, `cmd/omarket/` is
the CLI and TUI, `examples/` is the reference integrations. Keep `docs/SPEC.md`
in step with any change to the format or the API.
