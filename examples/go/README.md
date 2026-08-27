# hello-shareware — Go

The short one, because Go apps import the real thing:
[`github.com/aphexddb/omarket/license`](../../license) does the parsing and the
signature check, and [`main.go`](main.go) is mostly the app around it.

```bash
go run ./examples/go
go run ./examples/go ../testdata/hello-shareware.key
```

From this repo it builds as-is — it is part of the root module, so `go build
./...` and CI cover it. In your own project:

```bash
go get github.com/aphexddb/omarket
```

## Dropping it into your app

```go
import "github.com/aphexddb/omarket/license"

pub, _ := license.DecodePublicKey(platformPublicKey)

lic, err := license.Verify(strings.TrimSpace(string(raw)), pub)
if err == nil && lic.App == appID {
    // registered
}
```

`license.Verify` checks the Ed25519 signature over the exact payload bytes and
that the payload is format v1. It does **not** check the app id — that one is
yours to make, and skipping it lets any omarket license unlock your app. See
[the two ways to get this wrong](../README.md#two-ways-to-get-this-wrong).

## Notes

- **`os.UserConfigDir()`** is the same call the `omarket` client makes, so
  `filepath.Join(dir, "shareware", "licenses", appID+".key")` lands exactly
  where `omarket buy` wrote the key — on Linux, macOS, and Windows alike.
- **Two error kinds are worth telling apart.** `license.ErrInvalidFormat`
  means it isn't a SHRW1 key at all (a paste gone wrong, a truncated file);
  `license.ErrBadSignature` means someone edited or forged one. Both are
  `errors.Is`-comparable. This example collapses them into one nag; a real app
  might word them differently.
- **Those two sentinels are the whole taxonomy.** Undecodable base64 and a
  wrong-length signature both surface as `ErrInvalidFormat`, where the C, Rust,
  and Ruby examples say "malformed payload" or "malformed signature". Nothing
  is lost — the verdict is identical — but the diagnostics are coarser here.
- **The `license` package is pure stdlib** — `crypto/ed25519`,
  `encoding/base64`, `encoding/json`. Importing it does not drag the CLI's
  TUI dependencies into your binary.
