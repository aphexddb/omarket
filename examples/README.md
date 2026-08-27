# Examples — checking an omarket license in your app

Four implementations of the same tiny shareware app, `hello-shareware`. Each
one reads a license key off disk, verifies it offline, and unlocks a feature
when the key is genuine and belongs to it.

| | build | crypto | reads like |
|---|---|---|---|
| [c/](c) | `make` | OpenSSL `libcrypto` | the spec, spelled out byte by byte |
| [go/](go) | `go build ./examples/go` | the repo's [`license`](../license) package | four lines and you're done |
| [rust/](rust) | `cargo build --release` | `ed25519-dalek` | typed errors, no panics |
| [ruby/](ruby) | none — `ruby hello_shareware.rb` | stdlib `openssl` | no gems, no Gemfile |

They print the same report and turn away the same keys, so diffing any two
shows you the language and not much else.

## Run one right now

No purchase, no server, no network. `testdata/` holds a throwaway keypair and
a license signed with it:

```bash
cd examples/c && make demo
```

```bash
cd examples/go && SHAREWARE_PUBLIC_KEY=$(cat ../testdata/demo.pub) \
  go run . ../testdata/hello-shareware.key
```

```bash
cd examples/rust && SHAREWARE_PUBLIC_KEY=$(cat ../testdata/demo.pub) \
  cargo run -- ../testdata/hello-shareware.key
```

```bash
cd examples/ruby && SHAREWARE_PUBLIC_KEY=$(cat ../testdata/demo.pub) \
  ruby hello_shareware.rb ../testdata/hello-shareware.key
```

Point any of them at `../testdata/tampered.key` or `../testdata/other-app.key`
to watch a bad key get turned away.

## What a key is

```
SHRW1.<base64url(payload JSON)>.<base64url(ed25519 signature)>
```

Both segments are base64url with the padding stripped. The payload is flat
JSON:

```json
{
  "v": 1,
  "id": "lic_ff4274949540b3c7",
  "app": "hello-shareware",
  "email_sha256": "6a6c26195c3682faa816966af789717c3bfa834eee6c599d667d2b3429c27cfd",
  "issued_at": 1787811209,
  "kind": "personal"
}
```

The email is a SHA-256 of the lowercased, trimmed address — the platform never
hands your binary a buyer's email. `kind` is `personal`, `team`, or `test`
(what `omarket sell testkey` mints). There is no expiry field: a key is good
forever, which is the deal.

Full details in [docs/SPEC.md](../docs/SPEC.md#license-keys-shrw1).

## What every example does, in the same order

1. **Read the key file.** `omarket buy` writes it to
   `$XDG_CONFIG_HOME/shareware/licenses/<app>.key` (`~/.config/...` when
   `XDG_CONFIG_HOME` is unset), mode 0600, one key per file, trailing newline.
2. **Split on `.`** — three parts, first one `SHRW1`.
3. **Base64url-decode parts 2 and 3.** Signatures are exactly 64 bytes.
4. **Verify Ed25519 over the decoded payload bytes**, against the platform
   public key baked into your binary.
5. **Only then parse the JSON**, and check `v == 1`.
6. **Check `app` is yours.**

## Two ways to get this wrong

**Verifying over a re-encoding of the payload.** The signature covers the
exact bytes that arrived. Parse-then-re-serialize has to reproduce field order
and spacing byte for byte, and sooner or later it will not — a JSON library
upgrade is enough. Decode, verify the bytes, *then* parse. Every example here
does it in that order for that reason.

**Not checking `app`.** A `SHRW1` key for someone else's app carries a
perfectly good platform signature. If step 6 is missing, one $3 license for
anything on omarket unlocks your app too. Try it:
`./hello-shareware ../testdata/other-app.key`.

## The public key

```
vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko=
```

Standard base64, 32 raw bytes. It is also served at
`https://omarket.dev/api/pubkey`, but do not fetch it at runtime — bake it in.
That constant is the entire reason verification works on a plane. It is a
*public* key: shipping it inside your binary is the design, and no one can
mint licenses with it.

Every example honours `SHAREWARE_PUBLIC_KEY` as an override, which is how they
run against `testdata/` and how you'd point one at a local `sharewared`.

## Wiring it up for real

1. `omarket sell init && omarket sell claim your-app && omarket sell push`
2. `omarket sell testkey` — mints a `kind: "test"` license and saves it to
   `~/.config/shareware/licenses/your-app.key`.
3. Copy an example, change `APP_ID` to your app id, and run it. It should say
   registered.

Sell-side details are in the [root README](../README.md#sell).

## testdata/

| file | what it is |
|---|---|
| `demo.pub` | throwaway Ed25519 public key, standard base64 |
| `demo.secret` | its private half — **a demo key, not a platform secret** |
| `hello-shareware.key` | valid license, `kind: personal` |
| `other-app.key` | valid signature, `app: some-other-app` |
| `tampered.key` | `hello-shareware.key` with one payload character flipped |

Nothing signed by `demo.secret` is worth anything; it exists so these examples
run offline. Regenerate the lot with:

```bash
go run ./examples/testdata/gen
```
