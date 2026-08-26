# omarket

Shareware for the terminal age, built for Omarchy.

Try apps free, forever. Paying unlocks an offline, Ed25519-signed license key. No phone-home, no DRM, no accounts. 

Learn more at [omarchyshareware.com](https://github.com/aphexddb/omarchyshareware.com).

## Install

Download the latest release for Linux x86_64:

```bash
curl -sL https://github.com/aphexddb/omarket/releases/latest/download/omarket_linux_x86_64.tar.gz | tar xz omarket
sudo install -m755 omarket /usr/local/bin/omarket
```

darwin and windows archives (amd64/arm64) are published too — see the [latest release](https://github.com/aphexddb/omarket/releases/latest).

Or build from source with Go:

```bash
go install github.com/aphexddb/omarket/cmd/omarket@latest
```

## Buying shareware is easy

Install the binary (above), then:

```bash
omarket buy                 # plain-text catalog
omarket buy hello-shareware # Stripe checkout via URL/QR, polls, saves your key
```

Your license lands in `~/.config/shareware/licenses/<app>.key`. Apps verify it fully offline — the platform's Ed25519 public key ships baked into the `omarket` binary, so there's zero configuration on your end.

```bash
omarket                # TUI — browse, enter for detail, b to buy, i to install
omarket install <app>  # pacman -S (falls back to yay, or prints the command)
omarket licenses       # list stored keys, verified status
omarket verify <app>.key   # re-verify a key offline, anytime
```

The client talks to `https://omarket.dev` by default; point it elsewhere with `OMARKET_SERVER` or `--server`.

## Selling shareware is easy

```bash
omarket sell init          # once: instant, no Stripe needed
omarket sell claim my-app-name # generates template omarket.json manifest
omarket sell push          # reads omarket.json manifest: name, price, description. wont push a template version!
omarket sell testkey       # your app now shows "registered" locally
omarket sell payouts       # when ready to get paid: Stripe onboarding in browser
```

Claimed apps can be bought immediately by exact name; appearing in the browse catalog is curated by the platform. The platform fee is public via `GET /api/catalog`.

## Packages

- `license/` — the `SHRW1` key format: sign, verify, keygen. Pure stdlib  `crypto/ed25519`; verification works fully offline. Import this to check licenses in your own app.
- `client/` — catalog fetch, buy flow, license store; what the CLI is built on.

The license format and API contract live in [`docs/SPEC.md`](docs/SPEC.md).

## Build

```bash
go build ./...
```

Version lives in [`VERSION`](VERSION). Tag `v$(cat VERSION)` to cut a release
via GoReleaser.

## License

MIT
