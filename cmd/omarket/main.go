// Command omarket is the user-facing shareware market client: browse the
// catalog, install apps, buy licenses, and manage them, either via a
// full-screen TUI (bare `omarket`) or plain subcommands (SPEC §3).
package main

import (
	"fmt"
	"os"

	"github.com/aphexddb/omarket/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run dispatches to the five top-level commands (buy, sell, licenses,
// verify, version) plus a few hidden backward-compat aliases that still
// work but are no longer advertised in usage: `list` (buy with no args),
// `install`, and `info`.
func run(args []string) error {
	if len(args) == 0 {
		return runTUI()
	}

	switch args[0] {
	case "buy":
		return runBuy(args[1:])
	case "sell":
		return runSell(args[1:])
	case "licenses":
		return runLicenses(args[1:])
	case "verify":
		return runVerify(args[1:])
	case "version", "-v", "--version":
		fmt.Println(version.String())
		return nil
	case "-h", "--help", "help":
		usage()
		return nil

	// Hidden backward-compat aliases: functional, but dropped from usage.
	case "list":
		return runList(args[1:])
	case "info":
		return runInfo(args[1:])
	case "install":
		return runInstall(args[1:])

	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  omarket                                       browse the catalog (TUI)
  omarket buy [app] [-email x]                  browse the catalog, or buy an app
  omarket sell <cmd>                            manage what you're selling
    sell init                                     start selling: instant, no Stripe needed
    sell claim <app-id>                            claim an app id, generates omarket.json
    sell push                                      push omarket.json (name/description/price)
    sell testkey [app]                             mint yourself a local test license
    sell payouts                                   set up getting paid: Stripe onboarding in browser
    sell status                                    seller account + app status
  omarket licenses                              list stored license keys, verified status
  omarket verify <key|path|-> [-server <url>]   verify a license key offline (or against a server's key)
  omarket version                               print the version

  all subcommands accept -server <url> to override the configured server`)
}
