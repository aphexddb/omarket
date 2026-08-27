package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/aphexddb/omarket/client"
)

// invalidValue is what the table prints in place of a number that cannot be
// true: a negative price, a negative license count, a negative total.
//
// The alternative — rendering it faithfully as "$-0.01" or "-7" — is worse
// than useless, because it looks like data. A seller could reconcile
// against it, or file a bug about their earnings, when what actually
// happened is that the server sent something impossible. Naming it as
// invalid is the honest rendering, and it keeps one bad row from
// discrediting the rest of the table.
const invalidValue = "invalid"

// formatCents renders a cent amount as USD, refusing negatives.
func formatCents(cents int64) string {
	if cents < 0 {
		return invalidValue
	}
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}

// formatCount renders a license count, refusing negatives.
func formatCount(n int64) string {
	if n < 0 {
		return invalidValue
	}
	return fmt.Sprintf("%d", n)
}

// formatPriceOrWare renders what goes in a listing's price column. A priced
// app shows its price; a ware-only app shows its ware instead, because
// "$0.00" tells a seller nothing while "postcardware" tells them exactly
// what that listing asks of the people who take it.
func formatPriceOrWare(cents int64, ware string) string {
	if cents < 0 {
		return invalidValue
	}
	if cents == 0 {
		return client.WareOrDefault(ware)
	}
	return formatCents(cents)
}

// cell makes a server-supplied string safe to drop into a terminal table:
// no escape sequences to repaint the screen, and no newlines or tabs to
// forge extra rows and columns. Everything in a stats row — names, wares,
// ids — came over the network, so all of it goes through here.
func cell(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\t' || unicode.IsControl(r) || !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(cleaned)
}

// runSellStats implements `omarket sell stats`: how many licenses each of
// your apps has produced.
func runSellStats(args []string) error {
	fs := flag.NewFlagSet("sell stats", flag.ExitOnError)
	server := fs.String("server", "", "market server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	token, err := requireSellerToken()
	if err != nil {
		return err
	}

	c := client.NewClient(client.ResolveServer(*server))
	stats, err := c.GetSellerStats(context.Background(), token)
	if err != nil {
		return apiError("fetching stats", err)
	}

	return renderSellerStats(os.Stdout, stats)
}

// renderSellerStats writes the stats table to w. Split out from
// runSellStats so the layout can be tested without a server, which is where
// the interesting cases live: a free app, an unlisted one, and a row whose
// numbers can't be true.
func renderSellerStats(w io.Writer, stats client.SellerStats) error {
	if len(stats.Apps) == 0 {
		fmt.Fprintln(w, mutedStyle.Render("No apps yet — claim one with `omarket sell claim <app-id>`."))
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
	fmt.Fprintln(tw, "APP\tNAME\tPRICE\tLICENSES\tGROSS")
	for _, a := range stats.Apps {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			cell(a.ID),
			cell(a.Name),
			cell(formatPriceOrWare(int64(a.PriceUSDCents), a.Ware)),
			formatCount(a.Licenses),
			grossCell(a),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, summaryLine(stats))
	if note := unlistedNote(stats.Apps); note != "" {
		fmt.Fprintln(w, mutedStyle.Render(note))
	}
	return nil
}

// grossCell renders a row's gross. A listing that has sold nothing, and a
// ware-only listing that never had a price, both show a dash: zero dollars
// is not a number worth reading, and an em-dash keeps the column scannable
// for the rows that do have one.
func grossCell(a client.SellerAppStat) string {
	if a.GrossUSDCents == 0 {
		return "—"
	}
	return formatCents(a.GrossUSDCents)
}

// summaryLine is the one-line answer to "so how am I doing".
func summaryLine(stats client.SellerStats) string {
	apps := fmt.Sprintf("%d app", len(stats.Apps))
	if len(stats.Apps) != 1 {
		apps += "s"
	}
	line := fmt.Sprintf("%s licenses across %s", formatCount(stats.TotalLicenses), apps)
	if stats.TotalGrossUSDCents != 0 {
		line += " · " + formatCents(stats.TotalGrossUSDCents) + " gross"
	}
	return successStyle.Render(line)
}

// unlistedNote names the apps that aren't in the browse catalog, which is
// the usual explanation for a row sitting at zero. Claimed apps are always
// buyable by exact name; being listed is a separate, curated step, and a
// seller staring at a zero deserves to be told which one they're looking at.
func unlistedNote(apps []client.SellerAppStat) string {
	var hidden []string
	for _, a := range apps {
		if !a.Listed {
			hidden = append(hidden, cell(a.ID))
		}
	}
	if len(hidden) == 0 {
		return ""
	}
	verb := "isn't"
	if len(hidden) > 1 {
		verb = "aren't"
	}
	return fmt.Sprintf("%s %s in the browse catalog yet (still buyable by exact name): %s",
		plural(len(hidden), "app"), verb, strings.Join(hidden, ", "))
}

// plural renders "1 app" / "2 apps".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
