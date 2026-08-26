# omarchy-shareware

Shareware for the terminal age.

Devs ship real Arch packages. Users try everything, free, forever if they want. Paying unlocks an offline, Ed25519-signed license key — no phone-home, no DRM, no accounts. The platform takes a flat 5% via Stripe Connect and eats Stripe's processing fees out of that cut. Devs net 95%. One number.

The canonical instance lives at [omarket.dev](https://omarket.dev) — `omarket` points there by default. The code is MIT; run your own instance if you'd rather. This is a community project with no affiliation with Omarchy, Basecamp, or 37signals — it's just built for people who live in that terminal.

## Why not an app store

- **5% flat.** No tiers, no "featured placement" upsell, no 30% toll.
- **No gatekeeping.** No review queue. Curation happens via pull request against `catalog/`, same as any other patch.
- **Offline keys.** A license is a signed string you can verify with nothing but a public key and stdlib crypto. It works with the network off.
- **Source-included by default.** Listings that ship source get featured. This isn't a walled garden; it's shareware.

## Quickstart: users

```bash
omarket                # bubbletea TUI — browse, enter for detail, b to buy, i to install
omarket list            # plain-text catalog
omarket buy hello-shareware   # opens Stripe checkout, polls for completion, saves your key
omarket install hello-shareware   # pacman -S (falls back to yay, or just prints the command)
```

Licenses land in `~/.config/shareware/licenses/<app>.key`. Nothing to log into.

## Quickstart: devs

1. `omarket dev onboard -email you@example.com` — spins up a Stripe Express account, hands you an onboarding URL. Takes a few minutes.
2. Package your app: an Arch `PKGBUILD` (start from `packaging/PKGBUILD.template`) plus, optionally, a license check (three ways to do it, honor system included — see `docs/DEVELOPERS.md`).
3. Add `catalog/<id>.json` and open a PR. That's the review process.

See `docs/DEVELOPERS.md` for the full walkthrough, including the money math.

## Monorepo layout

Single Go module: `github.com/aphexddb/omarchy-shareware`.

| Path | What lives here |
|------|------|
| `license/` | License key format: sign, verify, keygen (pure stdlib `crypto/ed25519`) |
| `cmd/sharewarectl/` | CLI: keygen, sign, verify (platform + dev tooling) |
| `server/` | HTTP server logic: catalog, buy, webhook, purchase polling, dev onboarding |
| `cmd/sharewared/` | Server entrypoint |
| `client/` | `omarket` client logic: catalog fetch, buy flow, license store |
| `cmd/omarket/` | User-facing TUI/CLI |
| `catalog/` | App listings, one JSON file per app (curation via PR) |
| `packaging/` | PKGBUILD template + GitHub Action for devs |
| `examples/` | Example paid app, wired up end to end |
| `web/` | Static landing page, served by `sharewared` at `/` |
| `docs/` | `SPEC.md` (the contract) and `DEVELOPERS.md` (the walkthrough) |

## Build

```bash
go build ./...
```

## Docs

- [`docs/DEVELOPERS.md`](docs/DEVELOPERS.md) — get paid in 10 minutes
- [`docs/SPEC.md`](docs/SPEC.md) — the platform spec, authoritative

## License

MIT. See [`LICENSE.md`](LICENSE.md).
