# hello-shareware — Ruby

[`hello_shareware.rb`](hello_shareware.rb), stdlib only. No gems, no Gemfile,
no bundler, no build step. Ruby 2.6 or newer.

```bash
ruby hello_shareware.rb
ruby hello_shareware.rb ../testdata/hello-shareware.key
```

## Dropping it into your app

Copy `verify_license`, `ed25519_public_key`, and the `ED25519_SPKI_PREFIX`
constant. Change `APP_ID`.

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
  public key, which is padded standard base64.
- **The verify happens on the decoded string**, not on
  `JSON.generate(JSON.parse(payload))`. Ruby preserves insertion order today;
  betting your licensing on that is a bad trade.
