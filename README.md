# omarket

A CLI to publish shareware and buy a DRM-free license. Built for Omarchy;
works in any terminal.

Try the binary free. Pay for a key file. Keep the bits. The canonical
server is [omarket.dev](https://omarket.dev).

## Install on Omarchy

```bash
curl -fsSL https://raw.githubusercontent.com/aphexddb/omarket/master/install-omarchy | bash
```

The installer downloads the latest GitHub release and puts the binary in
`~/.local/bin`. Make sure that's on your `PATH`.

Set `OMARKET_PREFIX` to install somewhere other than `~/.local`, or
`OMARKET_VERSION` (e.g. `v0.1.0`) to pin a release instead of `latest`.

### Other systems

```bash
go install github.com/aphexddb/omarket/cmd/omarket@latest
```

## Buy

```bash
omarket buy                 # plain table of the catalog
omarket buy hello-shareware # prints a Stripe Checkout URL and QR, polls, writes your key
```

Paid → the license is written to `~/.config/shareware/licenses/<app>.key`.
It is a file: `SHRW1.payload.sig`, Ed25519-signed. Apps verify it against
the platform public key baked into the binary — offline, no account, no
phone-home after purchase. [`examples/`](examples) shows how, in four
languages.

```bash
omarket                # TUI — browse, enter for detail, b to buy, i to install
omarket install <app>  # omarchy pkg add, then pacman, then yay
omarket licenses       # list stored keys with verified status
omarket verify <key|path|->  # re-verify a key offline, any time
```

The client talks to `https://omarket.dev` by default; point it at another
server with `-server` or `OMARKET_SERVER`.

## Sell

```bash
omarket sell init            # create a seller account; token written to disk
omarket sell claim my-app    # claim the id; generates an omarket.json manifest
omarket sell push            # upload name, description, price, ware from omarket.json
omarket sell testkey         # mint a test license; your app shows registered
omarket sell payouts         # Stripe Express onboarding, when you want to get paid
```

The manifest carries `ware` — the "-ware" tradition your listing follows
(`shareware`, `beerware`, `coffeeware`, `charityware`, or one you invent) —
plus the one-line `comment` that says what you're asking for and the
`author` handle, pre-filled from your git config. `comment` and `author` are
required; `ware` defaults to `shareware`.

`push` refuses a manifest that still has template values. A pushed app is
buyable by exact name immediately; the browse catalog is curated by the
platform. The fee is a flat 5%, public via `GET /api/catalog`, and Stripe's
processing fee comes out of the platform's side.

## Check licenses in your app

Your app is not required to be written in Go — a `SHRW1` key is a signed
string, and verifying one is base64url-decode, Ed25519-verify, parse JSON,
check the app id. [`examples/`](examples) has the same tiny shareware app
four times over:

| | build | crypto |
|---|---|---|
| [`examples/c`](examples/c) | `make` | OpenSSL `libcrypto` |
| [`examples/go`](examples/go) | `go build ./examples/go` | the `license` package |
| [`examples/rust`](examples/rust) | `cargo build --locked --release` | `ed25519-dalek` |
| [`examples/ruby`](examples/ruby) | `bundle install` | stdlib `openssl` + pinned `base64` |

They print the same report and turn away the same keys, so diffing any two
shows the language and not much else. A demo keypair ships alongside them, so
you can run one right now:

```bash
make examples          # C, Go, Rust, Ruby against testdata
cd examples/c && make demo
```

## Packages

- `license/` — the `SHRW1` key format: sign, verify, keygen. Pure stdlib
  `crypto/ed25519`; verification is fully offline. Import this to check
  licenses in your own app.
- `client/` — catalog fetch, buy flow, license store; what the CLI is built on.
- `examples/` — reference license checks in C, Go, Rust, and Ruby, plus the
  demo fixtures they run against.

The license format and API contract live in [`docs/SPEC.md`](docs/SPEC.md).

## Build

```bash
go build ./...
```

Version lives in [`VERSION`](VERSION). Tag `v$(cat VERSION)` to cut a
release via GoReleaser.

## License

MIT
