// Command omarket is the user-facing shareware market client: browse the
// catalog, install apps, buy licenses, and manage them, either via a
// full-screen TUI (bare `omarket`) or plain subcommands (SPEC §4).
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

func run(args []string) error {
	if len(args) == 0 {
		return runTUI()
	}

	switch args[0] {
	case "list":
		return runList(args[1:])
	case "info":
		return runInfo(args[1:])
	case "install":
		return runInstall(args[1:])
	case "buy":
		return runBuy(args[1:])
	case "licenses":
		return runLicenses(args[1:])
	case "dev":
		return runDev(args[1:])
	case "version", "-v", "--version":
		fmt.Println(version.String())
		return nil
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  omarket                      browse the catalog (TUI)
  omarket list                 plain table of the catalog
  omarket info <app>
  omarket install <app>
  omarket buy <app> [-email x]
  omarket licenses
  omarket dev onboard -email x
  omarket version

  all subcommands accept -server <url> to override the configured server`)
}
