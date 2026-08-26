# omarket

A CLI to publish shareware and buy a DRM-free license. Built for Omarchy;
works in any terminal.

Try the binary free. Pay for a key file. Keep the bits. The canonical
server is [omarket.dev](https://omarket.dev).

## Install

Download the latest release for Linux x86_64:

```bash
curl -sL https://github.com/aphexddb/omarket/releases/latest/download/omarket_linux_x86_64.tar.gz | tar xz omarket
sudo install -m755 omarket /usr/local/bin/omarket
```

darwin and windows archives (amd64/arm64) are published too — see the
[latest release](https://github.com/aphexddb/omarket/releases/latest).
Or build from source:

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
phone-home after purchase.

```bash
omarket                # TUI — browse, enter for detail, b to buy, i to install
omarket install <app>  # pacman -S (falls back to yay, or prints the command)
omarket licenses       # list stored keys with verified status
omarket verify <key|path|->  # re-verify a key offline, any time
```

The client talks to `https://omarket.dev` by default; point it at another
server with `-server` or `OMARKET_SERVER`.

## Sell

```bash
omarket sell init            # create a seller account; token written to disk
omarket sell claim my-app    # claim the id; generates an omarket.json manifest
omarket sell push            # upload name, description, price from omarket.json
omarket sell testkey         # mint a test license; your app shows registered
omarket sell payouts         # Stripe Express onboarding, when you want to get paid
```

`push` refuses a manifest that still has template values. A pushed app is
buyable by exact name immediately; the browse catalog is curated by the
platform. The fee is a flat 5%, public via `GET /api/catalog`, and Stripe's
processing fee comes out of the platform's side.

## Packages

- `license/` — the `SHRW1` key format: sign, verify, keygen. Pure stdlib
  `crypto/ed25519`; verification is fully offline. Import this to check
  licenses in your own app.
- `client/` — catalog fetch, buy flow, license store; what the CLI is built on.

The license format and API contract live in [`docs/SPEC.md`](docs/SPEC.md).

## Build

```bash
go build ./...
```

Version lives in [`VERSION`](VERSION). Tag `v$(cat VERSION)` to cut a
release via GoReleaser.

## License

MIT
