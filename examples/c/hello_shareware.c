/*
 * hello-shareware — a complete, minimal shareware app in C.
 *
 * It looks for a license key on disk, verifies it offline against the omarket
 * platform public key, and unlocks a feature when the key is genuine and
 * belongs to this app. No account, no activation call, no phone-home:
 * everything below runs with the network unplugged.
 *
 * A SHRW1 key is three dot-separated fields:
 *
 *     SHRW1.<base64url(payload JSON)>.<base64url(ed25519 signature)>
 *
 * Both segments are base64url with the padding stripped. The signature covers
 * the *exact payload bytes* — decode segment 2, verify over those bytes, and
 * only then parse the JSON. Parsing first and verifying over a re-encoding of
 * the result is the classic way to break this: key order and spacing would
 * have to match byte for byte, and one day they will not.
 *
 *   Build:  cc -O2 -Wall -Wextra -o hello-shareware hello_shareware.c -lcrypto
 *   Needs:  OpenSSL 1.1.1 or newer (Ed25519 landed there).
 */

#define _POSIX_C_SOURCE 200809L

#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/* APP_ID must match the "app" field inside the license payload. This is what
   stops a valid key for someone else's app from unlocking yours. */
#define APP_ID "hello-shareware"
#define APP_NAME "Hello Shareware"

/* The omarket Ed25519 license-signing key, also served at
   https://omarket.dev/api/pubkey. Bake it into your binary — that is what
   makes verification offline. It is a public key: shipping it to users is the
   point, and it cannot be used to mint licenses. */
static const char PLATFORM_PUBLIC_KEY[] = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko=";

#define ED25519_PUBLIC_KEY_LEN 32
#define ED25519_SIGNATURE_LEN 64
#define KEY_MAX 4096
#define PAYLOAD_MAX 2048

struct license {
	char id[64];
	char app[96];
	char kind[32];
	long long issued_at;
};

/* ---------------------------------------------------------------- base64 */

/* b64_value maps one base64 character to its 6 bits, accepting both the
   standard (+/) and URL-safe (-_) alphabets — so one decoder handles the
   padded standard base64 of the public key and the unpadded base64url of the
   two key segments. Returns -1 for anything else. */
static int b64_value(unsigned char c)
{
	if (c >= 'A' && c <= 'Z') return c - 'A';
	if (c >= 'a' && c <= 'z') return c - 'a' + 26;
	if (c >= '0' && c <= '9') return c - '0' + 52;
	if (c == '+' || c == '-') return 62;
	if (c == '/' || c == '_') return 63;
	return -1;
}

/* b64_decode decodes len characters from in into out, writing at most cap
   bytes and storing the count in *out_len. Padding is optional. Returns 1 on
   success, 0 on an unexpected character or on overflow. */
static int b64_decode(const char *in, size_t len, unsigned char *out, size_t cap,
                      size_t *out_len)
{
	unsigned int acc = 0;
	int bits = 0;
	size_t n = 0;

	for (size_t i = 0; i < len; i++) {
		if (in[i] == '=') break; /* padding; nothing meaningful follows */
		int v = b64_value((unsigned char)in[i]);
		if (v < 0) return 0;
		acc = (acc << 6) | (unsigned int)v;
		bits += 6;
		if (bits >= 8) {
			bits -= 8;
			if (n >= cap) return 0;
			out[n++] = (unsigned char)((acc >> bits) & 0xFF);
		}
	}
	*out_len = n;
	return 1;
}

/* --------------------------------------------------------------- ed25519 */

/* ed25519_verify checks sig (64 bytes) over msg using the raw 32-byte public
   key pub. Ed25519 in OpenSSL is one-shot: EVP_DigestVerify, never the
   Update/Final pair. */
static int ed25519_verify(const unsigned char *pub, const unsigned char *msg,
                          size_t msg_len, const unsigned char *sig)
{
	EVP_PKEY *pkey = EVP_PKEY_new_raw_public_key(EVP_PKEY_ED25519, NULL, pub,
	                                             ED25519_PUBLIC_KEY_LEN);
	if (!pkey) return 0;

	EVP_MD_CTX *ctx = EVP_MD_CTX_new();
	int ok = 0;
	if (ctx && EVP_DigestVerifyInit(ctx, NULL, NULL, NULL, pkey) == 1)
		ok = EVP_DigestVerify(ctx, sig, ED25519_SIGNATURE_LEN, msg, msg_len) == 1;

	EVP_MD_CTX_free(ctx);
	EVP_PKEY_free(pkey);
	return ok;
}

/* ------------------------------------------------------------------ json */

/* The SHRW1 payload is a flat JSON object of short scalars — no nesting, no
   escapes, no arrays — so searching for the quoted field name is enough, and
   keeps this file dependency-free. Do not reach for these three in a program
   that parses JSON whose signature it has not already checked. */

static const char *json_field(const char *p, size_t n, const char *name)
{
	char pat[64];
	int len = snprintf(pat, sizeof pat, "\"%s\":", name);
	if (len <= 0 || (size_t)len >= sizeof pat) return NULL;

	for (size_t i = 0; i + (size_t)len <= n; i++)
		if (memcmp(p + i, pat, (size_t)len) == 0) {
			const char *v = p + i + len;
			while (v < p + n && (*v == ' ' || *v == '\t')) v++;
			return v;
		}
	return NULL;
}

static int json_string(const char *p, size_t n, const char *name, char *out, size_t cap)
{
	const char *v = json_field(p, n, name);
	if (!v || v >= p + n || *v != '"') return 0;

	size_t i = 0;
	for (v++; v < p + n && *v != '"'; v++) {
		if (i + 1 >= cap) return 0;
		out[i++] = *v;
	}
	if (v >= p + n) return 0;
	out[i] = '\0';
	return 1;
}

static int json_number(const char *p, size_t n, const char *name, long long *out)
{
	const char *v = json_field(p, n, name);
	if (!v) return 0;

	char buf[32];
	size_t i = 0;
	for (; v < p + n && (*v == '-' || (*v >= '0' && *v <= '9')); v++) {
		if (i + 1 >= sizeof buf) return 0;
		buf[i++] = *v;
	}
	if (i == 0) return 0;
	buf[i] = '\0';
	*out = strtoll(buf, NULL, 10);
	return 1;
}

/* --------------------------------------------------------------- license */

/* verify_license splits key, checks its signature against the base64 public
   key pub_b64, and fills *out. Returns NULL on success, or a short reason.
   It answers exactly one question — did the platform sign this? — and says
   nothing about which app the key is for. That check belongs to the caller. */
static const char *verify_license(const char *key, const char *pub_b64,
                                  struct license *out)
{
	/* 1. split SHRW1.<payload>.<signature> */
	const char *dot1 = strchr(key, '.');
	if (!dot1 || (size_t)(dot1 - key) != 5 || memcmp(key, "SHRW1", 5) != 0)
		return "not a SHRW1 key";
	const char *dot2 = strchr(dot1 + 1, '.');
	if (!dot2 || strchr(dot2 + 1, '.'))
		return "not a SHRW1 key";

	/* 2. decode the three binary pieces */
	unsigned char payload[PAYLOAD_MAX];
	unsigned char sig[ED25519_SIGNATURE_LEN + 1];
	unsigned char pub[ED25519_PUBLIC_KEY_LEN + 1];
	size_t payload_len, sig_len, pub_len;

	if (!b64_decode(dot1 + 1, (size_t)(dot2 - dot1 - 1), payload, sizeof payload,
	                &payload_len))
		return "malformed payload";
	if (!b64_decode(dot2 + 1, strlen(dot2 + 1), sig, sizeof sig, &sig_len) ||
	    sig_len != ED25519_SIGNATURE_LEN)
		return "malformed signature";
	if (!b64_decode(pub_b64, strlen(pub_b64), pub, sizeof pub, &pub_len) ||
	    pub_len != ED25519_PUBLIC_KEY_LEN)
		return "malformed public key";

	/* 3. the signature covers the payload bytes exactly as they arrived */
	if (!ed25519_verify(pub, payload, payload_len, sig))
		return "bad signature";

	/* 4. only now is the payload worth reading */
	const char *json = (const char *)payload;
	long long version = 0;
	if (!json_number(json, payload_len, "v", &version) || version != 1)
		return "unsupported license version";
	if (!json_string(json, payload_len, "id", out->id, sizeof out->id) ||
	    !json_string(json, payload_len, "app", out->app, sizeof out->app) ||
	    !json_string(json, payload_len, "kind", out->kind, sizeof out->kind) ||
	    !json_number(json, payload_len, "issued_at", &out->issued_at))
		return "incomplete payload";

	return NULL;
}

/* ------------------------------------------------------------------ main */

/* default_license_path builds $XDG_CONFIG_HOME/shareware/licenses/<app>.key,
   falling back to ~/.config — where `omarket buy` writes the key. */
static int default_license_path(char *out, size_t cap)
{
	const char *xdg = getenv("XDG_CONFIG_HOME");
	const char *home = getenv("HOME");
	int n;

	if (xdg && *xdg)
		n = snprintf(out, cap, "%s/shareware/licenses/%s.key", xdg, APP_ID);
	else if (home && *home)
		n = snprintf(out, cap, "%s/.config/shareware/licenses/%s.key", home, APP_ID);
	else
		return 0;

	return n > 0 && (size_t)n < cap;
}

/* read_key_file reads path into out and trims surrounding whitespace — the
   stored key file ends with a newline. */
static int read_key_file(const char *path, char *out, size_t cap)
{
	FILE *f = fopen(path, "rb");
	if (!f) return 0;

	size_t n = fread(out, 1, cap - 1, f);
	fclose(f);
	out[n] = '\0';

	while (n > 0 && (unsigned char)out[n - 1] <= ' ') out[--n] = '\0';
	size_t lead = 0;
	while (out[lead] && (unsigned char)out[lead] <= ' ') lead++;
	if (lead) memmove(out, out + lead, n - lead + 1);
	return 1;
}

static void nag(const char *reason)
{
	printf("  [ ] unregistered — %s\n", reason);
	printf("      buy a key:  omarket buy %s\n\n", APP_ID);
	printf("  the deluxe feature is off until this app is registered.\n");
}

int main(int argc, char **argv)
{
	printf("%s 1.0\n\n", APP_NAME);

	char path[1024];
	if (argc > 1)
		snprintf(path, sizeof path, "%s", argv[1]);
	else if (!default_license_path(path, sizeof path))
		snprintf(path, sizeof path, "%s.key", APP_ID);

	/* SHAREWARE_PUBLIC_KEY overrides the baked-in platform key — how you test
	   against a local stack, or against examples/testdata. */
	const char *pub_b64 = getenv("SHAREWARE_PUBLIC_KEY");
	if (!pub_b64 || !*pub_b64) pub_b64 = PLATFORM_PUBLIC_KEY;

	char key[KEY_MAX];
	char why[1200];

	if (!read_key_file(path, key, sizeof key)) {
		snprintf(why, sizeof why, "no license file at %s", path);
		nag(why);
		return 0;
	}

	struct license lic;
	const char *err = verify_license(key, pub_b64, &lic);
	if (err) {
		nag(err);
		return 0;
	}

	/* Genuine — but is it ours? Skip this and any paid omarket license, for
	   any app, unlocks yours. */
	if (strcmp(lic.app, APP_ID) != 0) {
		snprintf(why, sizeof why, "key is for \"%s\", not \"%s\"", lic.app, APP_ID);
		nag(why);
		return 0;
	}

	char issued[16] = "?";
	time_t t = (time_t)lic.issued_at;
	struct tm tm;
	if (gmtime_r(&t, &tm)) strftime(issued, sizeof issued, "%Y-%m-%d", &tm);

	printf("  [x] registered\n");
	printf("      license  %s\n", lic.id);
	printf("      kind     %s\n", lic.kind);
	printf("      issued   %s\n\n", issued);
	printf("  the deluxe feature is unlocked. Enjoy.\n");
	return 0;
}
