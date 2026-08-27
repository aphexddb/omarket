#!/usr/bin/env ruby
# frozen_string_literal: true

# hello-shareware — a complete, minimal shareware app in Ruby.
#
# It looks for a license key on disk, verifies it offline against the omarket
# platform public key, and unlocks a feature when the key is genuine and
# belongs to this app. No account, no activation call, no phone-home:
# everything below runs with the network unplugged.
#
# openssl and json are still default gems. base64 is not, as of Ruby 3.4 —
# the sibling Gemfile pins it. Ruby 2.6 or newer.
#
#   bundle install
#   ruby hello_shareware.rb
#   ruby hello_shareware.rb ../testdata/hello-shareware.key

begin
  require "bundler/setup" if File.file?(File.expand_path("Gemfile", __dir__))
rescue LoadError
end

begin
  require "base64"
rescue LoadError
  abort "hello_shareware.rb needs the base64 gem. From examples/ruby: bundle install"
end
require "json"
require "openssl"

# APP_ID must match the "app" field inside the license payload. This is what
# stops a valid key for someone else's app from unlocking yours.
APP_ID = "hello-shareware"
APP_NAME = "Hello Shareware"

# The omarket Ed25519 license-signing key, also served at
# https://omarket.dev/api/pubkey. Bake it into your app — that is what makes
# verification offline. It is a public key: shipping it to users is the point,
# and it cannot be used to mint licenses.
PLATFORM_PUBLIC_KEY = "vIODssCr6I8zWUTaK/IhdHzkoK5LTdEzbtIeXRnatko="

# Raised for every "this key does not unlock the app" outcome. A real app may
# want to tell a malformed key apart from a forged one; both mean unregistered.
class LicenseError < StandardError; end

# The SubjectPublicKeyInfo DER header for an Ed25519 key. OpenSSL::PKey.read
# wants a structured key, not 32 loose bytes, and the wrapper is constant:
#
#   SEQUENCE { SEQUENCE { OID 1.3.101.112 }, BIT STRING { 0x00 || key } }
#
# Ruby 3.4+ ships OpenSSL::PKey.new_raw_public_key, which does this for you.
# Twelve fixed bytes cost less than a minimum-version requirement.
ED25519_SPKI_PREFIX = [0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70,
                       0x03, 0x21, 0x00].pack("C*").freeze

def ed25519_public_key(raw32)
  OpenSSL::PKey.read(ED25519_SPKI_PREFIX + raw32)
end

# decode_b64url decodes an unpadded base64url segment, reporting which one
# went wrong rather than letting an ArgumentError escape.
def decode_b64url(str, what)
  Base64.urlsafe_decode64(str)
rescue ArgumentError
  raise LicenseError, "malformed #{what}"
end

# verify_license splits key, checks its Ed25519 signature against the base64
# public key pub_b64, and returns the decoded payload as a Hash.
#
# It answers exactly one question — did the platform sign this? — and says
# nothing about which app the key is for. That check belongs to the caller.
def verify_license(key, pub_b64)
  # 1. SHRW1.<base64url(payload JSON)>.<base64url(ed25519 signature)>, both
  #    segments unpadded.
  parts = key.split(".")
  raise LicenseError, "not a SHRW1 key" unless parts.length == 3 && parts[0] == "SHRW1"

  _, payload_b64, sig_b64 = parts

  payload = decode_b64url(payload_b64, "payload")
  signature = decode_b64url(sig_b64, "signature")
  raise LicenseError, "malformed signature" unless signature.bytesize == 64

  # 2. The signature covers the *exact payload bytes*. Verify over `payload`
  #    as decoded — never over JSON.generate(JSON.parse(payload)), which would
  #    have to reproduce key order and spacing byte for byte, and one day
  #    would not.
  pub_raw = Base64.strict_decode64(pub_b64)
  raise LicenseError, "malformed public key" unless pub_raw.bytesize == 32

  # `verify(nil, ...)` — Ed25519 signs the message itself, with no separate
  # digest step, so there is no digest to name here.
  pub = ed25519_public_key(pub_raw)
  raise LicenseError, "bad signature" unless pub.verify(nil, signature, payload)

  # 3. Only now is the payload worth reading.
  lic = JSON.parse(payload)
  raise LicenseError, "unsupported license version" unless lic["v"] == 1

  lic
rescue JSON::ParserError
  raise LicenseError, "malformed payload"
rescue ArgumentError, OpenSSL::PKey::PKeyError
  raise LicenseError, "malformed public key"
end

# default_license_path is $XDG_CONFIG_HOME/shareware/licenses/<app>.key,
# falling back to ~/.config — where `omarket buy` writes the key.
def default_license_path
  base = ENV["XDG_CONFIG_HOME"]
  base = File.join(Dir.home, ".config") if base.nil? || base.empty?
  File.join(base, "shareware", "licenses", "#{APP_ID}.key")
end

# SHAREWARE_PUBLIC_KEY overrides the baked-in platform key — how you test
# against a local stack, or against examples/testdata.
def public_key
  env = ENV["SHAREWARE_PUBLIC_KEY"]
  env.nil? || env.empty? ? PLATFORM_PUBLIC_KEY : env
end

def nag(reason)
  puts "  [ ] unregistered — #{reason}"
  puts "      buy a key:  omarket buy #{APP_ID}"
  puts
  puts "  the deluxe feature is off until this app is registered."
end

def main(argv)
  puts "#{APP_NAME} 1.0"
  puts

  path = argv[0] || default_license_path
  unless File.readable?(path)
    nag("no license file at #{path}")
    return
  end

  lic = verify_license(File.read(path).strip, public_key)

  # Genuine — but is it ours? Skip this and any paid omarket license, for any
  # app, unlocks yours.
  if lic["app"] != APP_ID
    nag(%(key is for "#{lic["app"]}", not "#{APP_ID}"))
    return
  end

  puts "  [x] registered"
  puts "      license  #{lic["id"]}"
  puts "      kind     #{lic["kind"]}"
  puts "      issued   #{Time.at(lic["issued_at"]).utc.strftime("%Y-%m-%d")}"
  puts
  puts "  the deluxe feature is unlocked. Enjoy."
rescue LicenseError => e
  nag(e.message)
end

main(ARGV) if $PROGRAM_NAME == __FILE__
