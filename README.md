# omarket

A CLI to publish shareware and buy a DRM-free license. Built for Omarchy;
works in any terminal.

Try the binary free. Pay for a key file. Keep the bits. The canonical
server is [omarket.dev](https://omarket.dev).

## Install on Omarchy

Clone the repository and run the Omarchy installer:

```bash
git clone https://github.com/aphexddb/omarket.git
cd omarket
./install-omarchy
```

The installer uses Omarchy's package helper for Go if it's missing, builds,
and installs to `~/.local/bin`. Make sure that's on your `PATH`.

Set `OMARKET_PREFIX` before running `install-omarchy` to use a prefix other
than `~/.local`.

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
phone-home after purchase.

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
