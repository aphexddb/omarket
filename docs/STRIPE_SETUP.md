# Stripe setup — from zero to first sale

Three dashboard artifacts map to three env vars:

| Dashboard | Env var |
|---|---|
| Connect enabled + platform profile | (no var — unlocks Express onboarding) |
| Developers → API keys → Secret key | `STRIPE_SECRET_KEY` |
| Developers → Webhooks → Signing secret | `STRIPE_WEBHOOK_SECRET` |

Do everything in **Test mode** first (toggle, top right of the dashboard).

## 1. Enable Connect

Sidebar → **Connect** → Get started. Answers that match how sharewared works:

- You are a **platform/marketplace**.
- You collect payments and **pay out to others**.
- Account type: **Express**.
- The platform sets pricing; charges are **destination charges** (the platform
  is the merchant of record and pays Stripe's processing fees — that's why the
  5% application fee is all-in for devs).

Complete the platform profile and accept the Connect agreement. Then
Settings → Connect → **Branding**: set the name and icon devs see during
Express onboarding.

## 2. Keys and webhook

- **Developers → API keys**: copy the Secret key → `STRIPE_SECRET_KEY`.
- **Developers → Webhooks → Add endpoint**:
  - URL: `https://<your-domain>/stripe/webhook`
  - Events: just `checkout.session.completed`
  - Regular account events, **not** "Connect application" events.
  - Copy the Signing secret → `STRIPE_WEBHOOK_SECRET`.

### Local development (no public URL yet)

Skip the dashboard webhook and use the Stripe CLI:

```bash
stripe login
stripe listen --forward-to localhost:8484/stripe/webhook
```

`stripe listen` prints a `whsec_...` — use that as `STRIPE_WEBHOOK_SECRET`.

## 3. Platform signing key

```bash
go run ./cmd/sharewarectl keygen
```

`PRIVATE=...` → `PLATFORM_SIGNING_KEY` in `.env`. Store the pair somewhere
durable and secret; the private key is the business.

## 4. Test the whole loop

```bash
cp .env.example .env   # fill in the three secrets
go build -o sharewared ./cmd/sharewared && ./sharewared   # or: source .env first
```

1. `omarket dev onboard -email you@example.com` — complete the test-mode
   Express onboarding (Stripe prefills everything in test mode).
2. Put the resulting `acct_...` into `catalog/hello-shareware.json` as
   `stripe_account`, restart sharewared.
3. `omarket buy hello-shareware` — open the checkout URL, pay with the test
   card `4242 4242 4242 4242` (any future expiry, any CVC).
4. The poll completes, the license key lands in
   `~/.config/shareware/licenses/hello-shareware.key`, and the Payments page
   of the dashboard shows the charge with a 5% application fee.

## 5. Go live

Repeat step 2 in **Live mode** on the deployed domain (real `sk_live_` key,
real webhook endpoint), swap the values in the server's environment, and run
one real purchase end to end before announcing anything. Live Express
onboarding requires devs' real identity/bank details — that part is Stripe's
problem, not yours.
