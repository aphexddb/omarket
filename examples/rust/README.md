# hello-shareware — Rust

[`src/main.rs`](src/main.rs), four dependencies, no `unwrap` on anything a
user can influence. Every failure is a `LicenseError` variant that turns into
one line of nag text.

```bash
cargo run --locked
SHAREWARE_PUBLIC_KEY=$(cat ../testdata/demo.pub) \
  cargo run --locked -- ../testdata/hello-shareware.key
make demo
cargo build --locked --release
```

`Cargo.lock` is committed, as it should be for a binary. Direct crate versions
in `Cargo.toml` are pinned to that lockfile; `--locked` refuses drift.

## Dropping it into your app

Copy `verify_license()`, the `License` struct, and the `LicenseError` enum.
Change `APP_ID`. That's it.

```rust
match verify_license(raw.trim(), PLATFORM_PUBLIC_KEY) {
    Ok(license) if license.app == APP_ID => { /* registered */ }
    _ => { /* nag */ }
}
```

`verify_license` answers one question — did the platform sign this? — and says
nothing about which app the key is for. The `license.app == APP_ID` arm is not
decoration; without it any omarket license unlocks your app. See
[the two ways to get this wrong](../README.md#two-ways-to-get-this-wrong).

## Notes

- **`verify_strict`, not `verify`.** `ed25519_dalek`'s strict variant rejects
  small-order public keys and non-canonical encodings. For license checks the
  cost is nothing and the failure mode it closes is real.
- **`serde_json::from_slice(&payload)` runs after the signature check**, on
  the decoded bytes — never on a re-serialization of a parsed struct. Field
  order and spacing would have to survive a `serde` upgrade unchanged, and one
  day they won't.
- **`try_into()` does the length check.** Decoding into `[u8; 64]` and
  `[u8; 32]` means a wrong-sized signature or key is a `Malformed` error at
  the point of decode, not a panic three lines later.
- **`issued_date()` is hand-rolled** — the civil-from-days algorithm, about
  ten lines — so the example doesn't pull in `chrono` or `time` just to print
  one date.
