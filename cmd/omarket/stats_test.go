package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// render runs renderSellerStats and returns the plain text it produced.
func render(t *testing.T, s client.SellerStats) string {
	t.Helper()
	var buf bytes.Buffer
	if err := renderSellerStats(&buf, s); err != nil {
		t.Fatalf("renderSellerStats: %v", err)
	}
	return buf.String()
}

// line returns the first rendered line containing want, for assertions that
// care which row a value landed on.
func line(t *testing.T, out, want string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, want) {
			return l
		}
	}
	t.Fatalf("no line containing %q in:\n%s", want, out)
	return ""
}

func TestRenderSellerStats(t *testing.T) {
	out := render(t, client.SellerStats{
		SellerID: "sel_abc",
		Apps: []client.SellerAppStat{
			{ID: "hello-tool", Name: "Hello Tool", PriceUSDCents: 900, Ware: "shareware", Listed: true, Licenses: 12, GrossUSDCents: 10800},
			{ID: "brand-new", Name: "Brand New", PriceUSDCents: 500, Ware: "shareware", Listed: true, Licenses: 0},
		},
		TotalLicenses:      12,
		TotalGrossUSDCents: 10800,
	})

	for _, want := range []string{"hello-tool", "Hello Tool", "$9.00", "12", "$108.00", "brand-new"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "$108.00") {
		t.Errorf("expected the total gross:\n%s", out)
	}
}

// TestRenderSellerStatsFreeAppShowsWare is the ware-only case: "$0.00" in a
// price column says nothing, while "postcardware" says exactly what this
// listing asks for.
func TestRenderSellerStatsFreeAppShowsWare(t *testing.T) {
	out := render(t, client.SellerStats{
		SellerID: "sel_abc",
		Apps: []client.SellerAppStat{
			{ID: "postcard-cli", Name: "Postcard CLI", PriceUSDCents: 0, Ware: "postcardware", Listed: true, Licenses: 3},
		},
		TotalLicenses: 3,
	})

	row := line(t, out, "postcard-cli")
	if !strings.Contains(row, "postcardware") {
		t.Errorf("a free app's row should name its ware: %q", row)
	}
	if strings.Contains(row, "$0.00") {
		t.Errorf("a free app should not be priced at $0.00: %q", row)
	}
	if !strings.Contains(row, "3") {
		t.Errorf("the license count is missing: %q", row)
	}
}

// TestRenderSellerStatsUnlistedIsCalledOut checks a seller wondering why
// nobody has bought their app is told it isn't in the browse catalog.
func TestRenderSellerStatsUnlistedIsCalledOut(t *testing.T) {
	out := render(t, client.SellerStats{
		SellerID: "sel_abc",
		Apps: []client.SellerAppStat{
			{ID: "listed-one", Name: "Listed", PriceUSDCents: 500, Ware: "shareware", Listed: true, Licenses: 1, GrossUSDCents: 500},
			{ID: "hidden-one", Name: "Hidden", PriceUSDCents: 500, Ware: "shareware", Listed: false},
		},
		TotalLicenses:      1,
		TotalGrossUSDCents: 500,
	})

	if !strings.Contains(out, "hidden-one") {
		t.Errorf("the unlisted app should still appear:\n%s", out)
	}
	if !strings.Contains(out, "browse catalog") {
		t.Errorf("expected a note about apps not in the browse catalog:\n%s", out)
	}
	if strings.Count(out, "listed-one") > 1 {
		t.Errorf("a listed app should not be named in the unlisted note:\n%s", out)
	}
}

// TestRenderSellerStatsEmpty checks a seller with nothing claimed gets a
// next step instead of an empty table.
func TestRenderSellerStatsEmpty(t *testing.T) {
	out := render(t, client.SellerStats{SellerID: "sel_abc", Apps: []client.SellerAppStat{}})

	if !strings.Contains(out, "sell claim") {
		t.Errorf("expected the empty state to point at `sell claim`:\n%s", out)
	}
}

// TestRenderSellerStatsRejectsNonsenseValues is the display half of the
// "handle the server failing things like -1 cents" requirement: a negative
// price or count must never be rendered as a plausible-looking number that
// someone could reconcile against.
func TestRenderSellerStatsRejectsNonsenseValues(t *testing.T) {
	out := render(t, client.SellerStats{
		SellerID: "sel_abc",
		Apps: []client.SellerAppStat{
			{ID: "bad-price", Name: "Bad Price", PriceUSDCents: -1, Ware: "shareware", Listed: true, Licenses: 2, GrossUSDCents: -2},
			{ID: "bad-count", Name: "Bad Count", PriceUSDCents: 500, Ware: "shareware", Listed: true, Licenses: -7},
		},
		TotalLicenses:      -5,
		TotalGrossUSDCents: -2,
	})

	if strings.Contains(out, "$-") {
		t.Errorf("a negative amount must not render as a dollar figure:\n%s", out)
	}
	if strings.Contains(out, "-7") || strings.Contains(out, "-5") {
		t.Errorf("a negative count must not render as a count:\n%s", out)
	}
	if !strings.Contains(out, invalidValue) {
		t.Errorf("expected nonsense values to be labelled %q:\n%s", invalidValue, out)
	}
}

// TestRenderSellerStatsSanitizesServerText checks a hostile name or ware
// can't repaint the terminal or forge extra rows in the table.
func TestRenderSellerStatsSanitizesServerText(t *testing.T) {
	out := render(t, client.SellerStats{
		SellerID: "sel_\x1b[31mred",
		Apps: []client.SellerAppStat{
			{ID: "evil", Name: "Evil\nfake-row\tcolumns", Ware: "wa\x1b[0mre", Listed: true, PriceUSDCents: 0, Licenses: 1},
		},
		TotalLicenses: 1,
	})

	if strings.Contains(out, "\x1b") {
		t.Errorf("escape sequences survived into the output:\n%q", out)
	}
	// One header line, one row, plus the summary — a name with a newline in
	// it must not become its own row.
	if strings.Contains(out, "\nfake-row") {
		t.Errorf("a newline in a name forged a table row:\n%s", out)
	}
}

// TestFormatCents pins the money formatter, including the case the rest of
// the table depends on.
func TestFormatCents(t *testing.T) {
	cases := map[int64]string{
		0:      "$0.00",
		1:      "$0.01",
		999:    "$9.99",
		10800:  "$108.00",
		-1:     invalidValue,
		-10800: invalidValue,
	}
	for in, want := range cases {
		if got := formatCents(in); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestFormatPriceOrWare checks the one substitution that makes a ware-only
// listing legible in a price column.
func TestFormatPriceOrWare(t *testing.T) {
	if got := formatPriceOrWare(0, "postcardware"); got != "postcardware" {
		t.Errorf("formatPriceOrWare(0, postcardware) = %q", got)
	}
	if got := formatPriceOrWare(0, ""); got != client.DefaultWare {
		t.Errorf("formatPriceOrWare(0, \"\") = %q, want the default ware", got)
	}
	if got := formatPriceOrWare(900, "beerware"); got != "$9.00" {
		t.Errorf("formatPriceOrWare(900, beerware) = %q, want the price", got)
	}
	if got := formatPriceOrWare(-1, "beerware"); got != invalidValue {
		t.Errorf("formatPriceOrWare(-1, beerware) = %q, want %q", got, invalidValue)
	}
}

// TestFormatCount is the counterpart guard for license counts.
func TestFormatCount(t *testing.T) {
	if got := formatCount(0); got != "0" {
		t.Errorf("formatCount(0) = %q", got)
	}
	if got := formatCount(12); got != "12" {
		t.Errorf("formatCount(12) = %q", got)
	}
	if got := formatCount(-1); got != invalidValue {
		t.Errorf("formatCount(-1) = %q, want %q", got, invalidValue)
	}
}
