package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/aphexddb/omarket/client"
)

// omarketAdminTokenEnv holds the platform operator's bearer token for admin
// commands. Deliberately not read from config/flags: this is a
// platform-operator-only escape hatch, not an end-user feature.
const omarketAdminTokenEnv = "OMARKET_ADMIN_TOKEN"

// runAdmin implements platform-operator-only curation commands. It's
// intentionally left out of the main usage text (see usage() in main.go)
// but stays fully functional for the platform creator.
func runAdmin(args []string) error {
	if len(args) == 0 || args[0] != "listed" {
		return fmt.Errorf("usage: omarket admin listed <app-id> <true|false>")
	}
	return runAdminListed(args[1:])
}

func runAdminListed(args []string) error {
	fs := flag.NewFlagSet("admin listed", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("usage: omarket admin listed <app-id> <true|false>")
	}
	id := fs.Arg(0)
	listed, err := strconv.ParseBool(fs.Arg(1))
	if err != nil {
		return fmt.Errorf("invalid listed value %q: must be true or false", fs.Arg(1))
	}

	token := os.Getenv(omarketAdminTokenEnv)
	if token == "" {
		return fmt.Errorf("%s is not set; export the platform admin token to run admin commands", omarketAdminTokenEnv)
	}

	c := client.NewClient(client.ResolveServer(*server))
	app, err := c.AdminSetListed(context.Background(), token, id, listed)
	if err != nil {
		return fmt.Errorf("setting listed for %q: %w", id, err)
	}

	fmt.Printf("%s listed=%v\n", app.ID, app.Listed)
	return nil
}
