# omarket

Shareware for the terminal age, built for Omarchy.

Try apps free, forever. Paying unlocks an offline, Ed25519-signed license key. No phone-home, no DRM, no accounts. 

Learn more at [omarchyshareware.com](https://github.com/aphexddb/omarchyshareware.com).

## Install

```bash
go install github.com/aphexddb/omarket/cmd/omarket@latest
```

AUR package: coming.

## Use

```bash
omarket                # TUI — browse, enter for detail, b to buy, i to install
omarket list           # plain-text catalog
omarket buy <app>      # Stripe checkout via URL/QR, polls, saves your key
omarket install <app>  # pacman -S (falls back to yay, or prints the command)
omarket licenses       # list stored keys, verified status
omarket dev onboard -email you@example.com   # start selling
```

Licenses land in `~/.config/shareware/licenses/<app>.key`. The client talks to `https://omarket.dev` by default; point it elsewhere with `OMARKET_SERVER` or `--server`.

## Packages

- `license/` — the `SHRW1` key format: sign, verify, keygen. Pure stdlib  `crypto/ed25519`; verification works fully offline. Import this to check licenses in your own app.
- `client/` — catalog fetch, buy flow, license store; what the CLI is built on.
- `cmd/sharewarectl` — keygen/sign/verify tooling for platform operators and devs who script their license checks.

The license format and API contract live in [`docs/SPEC.md`](docs/SPEC.md).

## Build

```bash
go build ./...
```

Version lives in [`VERSION`](VERSION). Tag `v$(cat VERSION)` to cut a release
via GoReleaser.

## License

MIT
