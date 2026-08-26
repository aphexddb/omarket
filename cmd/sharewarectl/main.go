// Command sharewarectl is the platform/dev CLI for the SHRW1 license format:
// generate keypairs, sign license keys, and verify them offline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aphexddb/omarchy-shareware/internal/version"
	"github.com/aphexddb/omarchy-shareware/license"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "keygen":
		err = runKeygen(os.Args[2:])
	case "sign":
		err = runSign(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version.String())
		return
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  sharewarectl keygen
  sharewarectl sign   -key <priv b64> -app <id> [-email x] [-kind personal]
  sharewarectl verify -pub <pub b64> -license <key or @file>
  sharewarectl version`)
}

func runKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	fs.Parse(args)

	pub, priv, err := license.GenerateKeypair()
	if err != nil {
		return err
	}
	fmt.Println("PUBLIC=" + license.EncodePublicKey(pub))
	fmt.Println("PRIVATE=" + license.EncodePrivateKey(priv))
	return nil
}

func runSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	key := fs.String("key", "", "private key (base64)")
	app := fs.String("app", "", "app id")
	email := fs.String("email", "", "buyer email")
	kind := fs.String("kind", "personal", "license kind")
	fs.Parse(args)

	if *key == "" || *app == "" {
		return fmt.Errorf("-key and -app are required")
	}

	priv, err := license.DecodePrivateKey(*key)
	if err != nil {
		return fmt.Errorf("decoding private key: %w", err)
	}

	l := license.NewLicense(*app, *email, *kind)
	out, err := license.Sign(l, priv)
	if err != nil {
		return fmt.Errorf("signing: %w", err)
	}
	fmt.Println(out)
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pubStr := fs.String("pub", "", "public key (base64)")
	lic := fs.String("license", "", "license key, or @file to read it from")
	fs.Parse(args)

	if *pubStr == "" || *lic == "" {
		return fmt.Errorf("-pub and -license are required")
	}

	pub, err := license.DecodePublicKey(*pubStr)
	if err != nil {
		return fmt.Errorf("decoding public key: %w", err)
	}

	key := *lic
	if strings.HasPrefix(key, "@") {
		b, err := os.ReadFile(key[1:])
		if err != nil {
			return fmt.Errorf("reading license file: %w", err)
		}
		key = strings.TrimSpace(string(b))
	}

	l, err := license.Verify(key, pub)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
