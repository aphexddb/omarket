//! hello-shareware — a complete, minimal shareware app in Rust.
//!
//! It looks for a license key on disk, verifies it offline against the omarket
//! platform public key, and unlocks a feature when the key is genuine and
//! belongs to this app. No account, no activation call, no phone-home:
//! everything below runs with the network unplugged.
//!
//! ```text
//! cargo run
//! cargo run -- ../testdata/hello-shareware.key
//! ```

use std::fmt;
use std::path::PathBuf;

use base64::engine::general_purpose::{STANDARD, URL_SAFE_NO_PAD};
use base64::Engine;
use ed25519_dalek::{Signature, VerifyingKey, SIGNATURE_LENGTH};
use serde::Deserialize;

/// Must match the `app` field inside the license payload. This is what stops a
/// valid key for someone else's app from unlocking yours.
const APP_ID: &str = "hello-shareware";
const APP_NAME: &str = "Hello Shareware";

/// The omarket Ed25519 license-signing key, also served at
/// <https://omarket.dev/api/pubkey>. Bake it into your binary — that is what
/// makes verification offline. It is a public key: shipping it to users is the
/// point, and it cannot be used to mint licenses.
const PLATFORM_PUBLIC_KEY: &str = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko=";

/// The signed payload of a SHRW1 key. `email_sha256` is a hash, never the
/// address — the platform does not hand your buyers' emails to your binary.
#[derive(Debug, Deserialize)]
struct License {
    v: u32,
    id: String,
    app: String,
    #[allow(dead_code)]
    email_sha256: String,
    issued_at: i64,
    kind: String,
}

/// Every "this key does not unlock the app" outcome. A real app may want to
/// tell a malformed key apart from a forged one; both mean unregistered.
#[derive(Debug)]
enum LicenseError {
    NotShrw1,
    Malformed(&'static str),
    BadSignature,
    UnsupportedVersion(u32),
}

impl fmt::Display for LicenseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LicenseError::NotShrw1 => write!(f, "not a SHRW1 key"),
            LicenseError::Malformed(what) => write!(f, "malformed {what}"),
            LicenseError::BadSignature => write!(f, "bad signature"),
            LicenseError::UnsupportedVersion(v) => write!(f, "unsupported license version {v}"),
        }
    }
}

/// Checks a SHRW1 key against the base64 public key `pub_b64` and returns the
/// decoded payload.
///
/// It answers exactly one question — did the platform sign this? — and says
/// nothing about which app the key is for. That check belongs to the caller.
fn verify_license(key: &str, pub_b64: &str) -> Result<License, LicenseError> {
    // 1. SHRW1.<base64url(payload JSON)>.<base64url(ed25519 signature)>, both
    //    segments unpadded.
    let parts: Vec<&str> = key.split('.').collect();
    if parts.len() != 3 || parts[0] != "SHRW1" {
        return Err(LicenseError::NotShrw1);
    }

    let payload = URL_SAFE_NO_PAD
        .decode(parts[1])
        .map_err(|_| LicenseError::Malformed("payload"))?;
    let sig_bytes: [u8; SIGNATURE_LENGTH] = URL_SAFE_NO_PAD
        .decode(parts[2])
        .map_err(|_| LicenseError::Malformed("signature"))?
        .try_into()
        .map_err(|_| LicenseError::Malformed("signature"))?;

    let pub_bytes: [u8; 32] = STANDARD
        .decode(pub_b64)
        .map_err(|_| LicenseError::Malformed("public key"))?
        .try_into()
        .map_err(|_| LicenseError::Malformed("public key"))?;
    let verifying_key =
        VerifyingKey::from_bytes(&pub_bytes).map_err(|_| LicenseError::Malformed("public key"))?;

    // 2. The signature covers the *exact payload bytes*. Verify over `payload`
    //    as decoded — never over a re-serialization of the parsed struct, which
    //    would have to reproduce field order and spacing byte for byte, and one
    //    day would not.
    verifying_key
        .verify_strict(&payload, &Signature::from_bytes(&sig_bytes))
        .map_err(|_| LicenseError::BadSignature)?;

    // 3. Only now is the payload worth parsing.
    let license: License =
        serde_json::from_slice(&payload).map_err(|_| LicenseError::Malformed("payload"))?;
    if license.v != 1 {
        return Err(LicenseError::UnsupportedVersion(license.v));
    }
    Ok(license)
}

/// `$XDG_CONFIG_HOME/shareware/licenses/<app>.key`, falling back to
/// `~/.config` — where `omarket buy` writes the key.
fn default_license_path() -> PathBuf {
    let base = std::env::var_os("XDG_CONFIG_HOME")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".config")))
        .unwrap_or_default();
    base.join("shareware")
        .join("licenses")
        .join(format!("{APP_ID}.key"))
}

/// `SHAREWARE_PUBLIC_KEY` overrides the baked-in platform key — how you test
/// against a local stack, or against `examples/testdata`.
fn public_key() -> String {
    match std::env::var("SHAREWARE_PUBLIC_KEY") {
        Ok(k) if !k.is_empty() => k,
        _ => PLATFORM_PUBLIC_KEY.to_string(),
    }
}

fn nag(reason: &str) {
    println!("  [ ] unregistered — {reason}");
    println!("      buy a key:  omarket buy {APP_ID}");
    println!();
    println!("  the deluxe feature is off until this app is registered.");
}

fn main() {
    println!("{APP_NAME} 1.0\n");

    let path = std::env::args_os()
        .nth(1)
        .map(PathBuf::from)
        .unwrap_or_else(default_license_path);

    let raw = match std::fs::read_to_string(&path) {
        Ok(raw) => raw,
        Err(_) => return nag(&format!("no license file at {}", path.display())),
    };

    let license = match verify_license(raw.trim(), &public_key()) {
        Ok(license) => license,
        Err(err) => return nag(&err.to_string()),
    };

    // Genuine — but is it ours? Skip this and any paid omarket license, for any
    // app, unlocks yours.
    if license.app != APP_ID {
        return nag(&format!("key is for {:?}, not {:?}", license.app, APP_ID));
    }

    println!("  [x] registered");
    println!("      license  {}", license.id);
    println!("      kind     {}", license.kind);
    println!("      issued   {}", issued_date(license.issued_at));
    println!();
    println!("  the deluxe feature is unlocked. Enjoy.");
}

/// Formats a Unix timestamp as YYYY-MM-DD (UTC) using the civil-from-days
/// algorithm, so the example stays at four dependencies instead of five.
fn issued_date(unix: i64) -> String {
    let days = unix.div_euclid(86_400);
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = yoe + era * 400 + i64::from(month <= 2);
    format!("{year:04}-{month:02}-{day:02}")
}
