# Get paid in 10 minutes

This is the whole dev-facing workflow: onboard, package, wire up a license check (optional), list. No accounts on our end, no review queue — curation happens via pull request.

## 1. Onboard with Stripe (2 minutes)

```bash
omarket dev onboard -email you@example.com
```

This calls `POST /api/dev/onboard`, which creates a Stripe Express account and hands back an onboarding URL:

```json
{"account":"acct_...","onboarding_url":"https://connect.stripe.com/..."}
```

Open the URL, fill in the Stripe Express form (bank details, identity — the usual). When it's done you have a `stripe_account` id. That's the only "account" in this whole system, and it belongs to Stripe, not us.

## 2. Write a PKGBUILD (5 minutes)

Start from [`packaging/PKGBUILD.template`](../packaging/PKGBUILD.template). It's a standard Go-app PKGBUILD: `go build` in `build()`, install the binary and `LICENSE` in `package()`. Fill in `pkgname`, `pkgver`, `pkgdesc`, `url`, `source`, and `sha256sums`. See `examples/hello-shareware/PKGBUILD` for a filled-in copy.

Add a release workflow so tags produce packages automatically — copy [`packaging/release.yml`](../packaging/release.yml) into your repo's `.github/workflows/`.

## 3. Gate it, nag it, or don't (your call)

A license key is a signed string: `SHRW1.<payload>.<signature>`, Ed25519, verified offline against the platform's public key. Three legitimate ways to use one — pick based on how much friction you want:

**(a) Go apps: import `license` and verify directly.**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aphexddb/omarchy-shareware/license"
)

// Platform public key, base64 std-encoded. Get the real value from the
// platform operator — this is a placeholder.
const platformPublicKey = "REPLACE_WITH_PLATFORM_PUBLIC_KEY_BASE64"

func checkLicense(appID string) *license.License {
	pub, err := license.DecodePublicKey(platformPublicKey)
	if err != nil {
		return nil // misconfigured build, treat as unregistered
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	keyPath := filepath.Join(dir, "shareware", "licenses", appID+".key")

	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil // no key on disk — unregistered, not an error
	}

	lic, err := license.Verify(string(raw), pub)
	if err != nil {
		return nil // bad or forged key — treat as unregistered
	}
	return lic
}

func main() {
	if lic := checkLicense("your-app-id"); lic != nil {
		fmt.Printf("registered to license %s\n", lic.ID)
	} else {
		fmt.Println("Unregistered — buy a key: omarket buy your-app-id")
	}
	// keep running either way — this is shareware, not a lock
}
```

`license.Verify` returns `license.ErrInvalidFormat` if the string isn't `SHRW1.x.y`, and `license.ErrBadSignature` if the signature doesn't check out against the given public key. Both just mean: treat the user as unregistered.

**(b) Any other language: shell out to `sharewarectl verify`.**

```bash
sharewarectl verify -pub "$PLATFORM_PUBLIC_KEY" \
  -license "@$HOME/.config/shareware/licenses/your-app-id.key"
```

Exit code `0` means valid (payload JSON printed to stdout); exit `1` means invalid or missing. Works from a shell script, Python, Rust, whatever — `sharewarectl` is a static binary, no Go toolchain required at runtime.

**(c) Honor system: nag, don't gate.**

Print a one-line reminder ("buy a key: omarket buy your-app-id") on every run, or once a day, and change nothing else. No key check at all. This is a completely legitimate choice — shareware has run on the honor system since the BBS era, and some devs would rather ship the goodwill than the gate.

Whichever you pick, never phone home to check a license. Verification is local and offline, full stop.

## 4. List it: add `catalog/<id>.json`

One file per app in `catalog/`, filename `<id>.json`. Open a PR — that's the entire review process; no queue, no approval wait beyond normal code review.

Every field, per SPEC §2:

| Field | Type | Notes |
|---|---|---|
| `id` | string | Must match the filename (`<id>.json`). Stable — this is the primary key everywhere (license `app` field, `omarket install`, etc). |
| `name` | string | Display name. |
| `description` | string | One-line-ish. Shown in `omarket list` / TUI. |
| `version` | string | Your app's version, informational — not enforced by the platform. |
| `homepage` | string | URL. |
| `source` | string | URL to source. Required in spirit for `"source-included"` kind. |
| `pkgname` | string | Arch package name. `omarket install` runs `sudo pacman -S <pkgname>` (falls back to `yay -S`; on non-Arch, prints the command instead of running it). |
| `price_cents` | int | `0` means free — no buy flow at all, `omarket buy` is a no-op. |
| `currency` | string | Lowercase ISO code, e.g. `"usd"`. |
| `stripe_account` | string | Your Stripe Connect account id (`acct_...`) from step 1. Required when `price_cents > 0`. |
| `kind` | string | `"source-included"` (featured tier) or `"closed"`. |
| `tags` | []string | Free-form, used for browsing/filtering. |

Example (`catalog/hello-shareware.json`, the demo app):

```json
{
  "id": "hello-shareware",
  "name": "Hello Shareware",
  "description": "The demo app: proves the try-then-buy loop end to end.",
  "version": "1.0.0",
  "homepage": "https://github.com/aphexddb/omarchy-shareware",
  "source": "https://github.com/aphexddb/omarchy-shareware/tree/master/examples/hello-shareware",
  "pkgname": "hello-shareware",
  "price_cents": 500,
  "currency": "usd",
  "stripe_account": "acct_REPLACE_ME",
  "kind": "source-included",
  "tags": ["demo", "example"]
}
```

## The money math

Say you price your app at **$9.00** (`price_cents: 900`).

- Buyer pays $9.00 through Stripe Checkout.
- Platform's application fee: `900 * 5 / 100 = 45` cents, taken via `payment_intent_data.application_fee_amount`.
- It's a destination charge — the platform is merchant of record, so Stripe's processing fee comes out of that 45-cent cut, not yours.
- **You net $8.55.** Not $8.55 minus a processor fee — $8.55, full stop.

No subscriptions, no tiers, no "pro plan" for a better rate. One number: 95% of the sticker price, every time.
