# hello-shareware

The demo app. Proves the try-then-buy loop end to end: it runs unregistered
with a nag, and prints your license ID once you've bought a key.

## Run it

```bash
go build ./examples/hello-shareware
./hello-shareware
```

Unregistered:

```
Unregistered — buy a key: omarket buy hello-shareware
hello, shareware.
```

Registered (after `omarket buy hello-shareware`):

```
registered to license lic_a1b2c3d4e5f60718
hello, shareware.
```

## How it checks

`main.go` reads `~/.config/shareware/licenses/hello-shareware.key` (via
`os.UserConfigDir()`) and verifies it with `license.Verify` against the
platform's public key — option (a) from `docs/DEVELOPERS.md`. No key, a bad
key, or a missing/invalid public key all fall through to "unregistered." The
app never refuses to run.

Listed in `catalog/hello-shareware.json`, packaged by the `PKGBUILD` in this
directory (built from `packaging/PKGBUILD.template`).
