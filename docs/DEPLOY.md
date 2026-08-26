# Deploying the canonical instance (omarket.dev)

One small VPS, one binary, Caddy in front. No database server, no containers.

## 0. DNS

Point `A`/`AAAA` records for `omarket.dev` (and `www`) at the VPS.

## 1. Build

Cross-compile from anywhere (pure Go, CGO not required):

```bash
GOOS=linux GOARCH=amd64 go build -o sharewared ./cmd/sharewared
```

## 2. Server layout

```bash
sudo useradd --system --home /srv/shareware --shell /usr/sbin/nologin shareware
sudo mkdir -p /srv/shareware/{catalog,web}
# copy up: sharewared binary, catalog/*.json, web/*, and a filled-in .env
sudo chown -R shareware:shareware /srv/shareware
sudo chmod 600 /srv/shareware/.env
```

`.env` (see `.env.example`): `BASE_URL=https://omarket.dev`, live Stripe keys, and the production `PLATFORM_SIGNING_KEY`. Keep an offline backup of the signing keypair — it cannot be rotated without stranding issued licenses.

## 3. systemd + Caddy

```bash
sudo cp deploy/sharewared.service /etc/systemd/system/
sudo systemctl enable --now sharewared
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile   # or merge into an existing one
sudo systemctl reload caddy
```

Caddy terminates TLS (automatic certificates) and proxies to `:8484`.

## 4. Stripe webhook (live mode)

Dashboard → Developers → Webhooks → Add endpoint: `https://omarket.dev/stripe/webhook`, event `checkout.session.completed`. Put the live `whsec_...` in `.env` and `sudo systemctl restart sharewared`.

## 5. Verify

```bash
curl https://omarket.dev/healthz
curl https://omarket.dev/catalog.json
```

Then one real end-to-end purchase (smallest listed app) before announcing.

## Updating the catalog

Catalog changes are merged via PR, then synced to the server (rsync the `catalog/` dir, restart sharewared). Automating that with a GitHub Action deploy hook is the natural next step once listings pick up.
