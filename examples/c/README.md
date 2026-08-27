# hello-shareware — C

One file, ~320 lines, one library. [`hello_shareware.c`](hello_shareware.c)
spells the [SHRW1 format](../README.md#what-a-key-is) out in full: base64url
decoder, Ed25519 verify, and just enough JSON to read six flat fields. If you
want to know exactly what a license check is, read this one first.

```bash
make
./hello-shareware
```

Needs OpenSSL 1.1.1 or newer — that is where `EVP_PKEY_ED25519` landed. On
Debian/Ubuntu that's `libssl-dev`; on Arch, `openssl`.

```bash
make demo     # run against ../testdata: valid, tampered, wrong app
make clean
```

## Dropping it into your app

Take `verify_license()` and the two helpers above it — `b64_decode()` and
`ed25519_verify()` — change `APP_ID`, and link `-lcrypto`. That's the whole
integration.

```c
struct license lic;
const char *err = verify_license(key, PLATFORM_PUBLIC_KEY, &lic);
if (!err && strcmp(lic.app, APP_ID) == 0) {
    /* registered */
}
```

`verify_license()` deliberately does not check the app id. It answers one
question — did the platform sign this? — and leaves the "is it mine?" check
where you can see it. Both halves are required; see
[the two ways to get this wrong](../README.md#two-ways-to-get-this-wrong).

## Notes

- **Ed25519 is one-shot.** `EVP_DigestVerify()`, never
  `EVP_DigestVerifyUpdate()` / `EVP_DigestVerifyFinal()`. Pass `NULL` for the
  digest in `EVP_DigestVerifyInit()`: Ed25519 hashes the message itself.
- **One base64 decoder covers both alphabets.** The public key is standard
  base64 (`+/`, padded); the key segments are base64url (`-_`, unpadded).
  `b64_value()` accepts either and `b64_decode()` treats padding as optional,
  so there is one function instead of two.
- **`json_string()` and `json_number()` are not a JSON parser.** They scan for
  a quoted field name in a flat object of short scalars, which is all a SHRW1
  payload ever is. They run *after* the signature check, on bytes the platform
  signed. Do not reuse them on untrusted input.
- **No allocation.** Fixed buffers throughout, so there is nothing to leak and
  no failure path that needs unwinding.
