# hello-shareware — Ruby

[`hello_shareware.rb`](hello_shareware.rb). openssl and json are default gems;
`base64` is not, as of Ruby 3.4. The [Gemfile](Gemfile) pins `base64` at
`0.3.0`. Ruby 2.6 or newer.

```bash
bundle install
ruby hello_shareware.rb
SHAREWARE_PUBLIC_KEY=$(cat ../testdata/demo.pub) \
  ruby hello_shareware.rb ../testdata/hello-shareware.key
make demo
```

## Dropping it into your app

Copy `verify_license`, `ed25519_public_key`, and the `ED25519_SPKI_PREFIX`
constant. Change `APP_ID`. Add `gem "base64", "0.3.0"` to your Gemfile if
you are on Ruby 3.4 or newer.

```ruby
lic = verify_license(File.read(path).strip, PLATFORM_PUBLIC_KEY)
registered = lic["app"] == APP_ID
rescue LicenseError
  registered = false
```

`verify_license` answers one question — did the platform sign this? — and says
nothing about which app the key is for. That `lic["app"] == APP_ID` line is
load-bearing; without it any omarket license unlocks your app. See
[the two ways to get this wrong](../README.md#two-ways-to-get-this-wrong).

## Notes

- **Twelve bytes instead of a version requirement.** Ruby 3.4+ has
  `OpenSSL::PKey.new_raw_public_key("ED25519", raw)`, but the openssl gem
  bundled with Ruby 3.2 does not. Wrapping the 32 raw bytes in the fixed
  Ed25519 `SubjectPublicKeyInfo` DER header and handing that to
  `OpenSSL::PKey.read` works on every Ruby back to 2.6. The header never
  changes — it's an ASN.1 sequence around OID 1.3.101.112.
- **`verify(nil, sig, msg)`.** The `nil` is the digest, and Ed25519 doesn't
  take one: it hashes the message internally. Passing `OpenSSL::Digest::SHA256`
  here is an error, not a hardening.
- **`Base64.urlsafe_decode64` handles missing padding** as of Ruby 2.3, which
  is what SHRW1 segments have. `strict_decode64` is the right call for the
  public key, which is padded standard base64. Ruby 3.4 extracted the `base64`
  default gem; this directory's Gemfile pins `0.3.0` so `bundle install` is
  enough.
- **The verify happens on the decoded string**, not on
  `JSON.generate(JSON.parse(payload))`. Ruby preserves insertion order today;
  betting your licensing on that is a bad trade.
