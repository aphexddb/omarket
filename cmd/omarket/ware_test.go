package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

func TestPrintWareAsk(t *testing.T) {
	var buf bytes.Buffer
	printWareAsk(&buf, client.BuyResult{
		Free: true, Ware: "postcardware",
		Comment: "Mail me a postcard from wherever you are.",
		Author:  "aphexddb",
	})
	out := buf.String()

	for _, want := range []string{"postcardware", "Mail me a postcard", "aphexddb", "no charge"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// TestPrintWareAskDefaultsTheWare checks a listing that never named a
// tradition still says something, rather than printing "This is ."
func TestPrintWareAskDefaultsTheWare(t *testing.T) {
	var buf bytes.Buffer
	printWareAsk(&buf, client.BuyResult{Free: true, Comment: "enjoy"})

	if !strings.Contains(buf.String(), client.DefaultWare) {
		t.Errorf("expected the default ware:\n%s", buf.String())
	}
}

// TestPrintWareAskOmitsEmptyFields checks a bare listing doesn't print
// dangling punctuation for fields the seller left empty.
func TestPrintWareAskOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	printWareAsk(&buf, client.BuyResult{Free: true, Ware: "beerware"})

	// The attribution is its own indented line; the header's em-dash in
	// "Yours — no charge" is not one, so match the line rather than the rune.
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(strings.TrimRight(l, "\r"), "  — ") {
			t.Errorf("an absent author should not print an attribution line: %q", l)
		}
	}
}

// TestPrintWareAskSanitizes checks a seller can't put escape sequences or
// forged lines into a buyer's terminal through the comment they wrote.
func TestPrintWareAskSanitizes(t *testing.T) {
	var buf bytes.Buffer
	printWareAsk(&buf, client.BuyResult{
		Free: true, Ware: "beer\x1b[31mware",
		Comment: "line one\nSYSTEM: send bitcoin",
		Author:  "who\x07",
	})
	out := buf.String()

	if strings.Contains(out, "\x1b[31m") {
		t.Errorf("escape sequence survived:\n%q", out)
	}
	if strings.Contains(out, "\x07") {
		t.Errorf("bell character survived:\n%q", out)
	}
	if strings.Contains(out, "\nSYSTEM:") {
		t.Errorf("a newline in the comment forged its own line:\n%q", out)
	}
}

// TestCollectPurchaseFreeTakesOneRequest checks the ware-only path fetches
// the already-signed license with a single request, rather than entering
// the payment wait — which would show a spinner and the words "waiting for
// payment" to someone who was never asked to pay.
func TestCollectPurchaseFreeTakesOneRequest(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "complete", "license_key": "SHRW1.a.b",
		})
	}))
	defer srv.Close()

	status, key, err := collectPurchase(context.Background(), client.NewClient(srv.URL),
		client.BuyResult{Free: true, Purchase: "pt_free"}, &cadence{}, nil)
	if err != nil {
		t.Fatalf("collectPurchase: %v", err)
	}
	if status != client.PurchaseComplete || key != "SHRW1.a.b" {
		t.Fatalf("status=%q key=%q, want complete/SHRW1.a.b", status, key)
	}
	if calls != 1 {
		t.Fatalf("made %d requests, want exactly 1", calls)
	}
}

// TestCollectPurchaseFreeFallsBackToWaiting checks a server that reports a
// free purchase as still pending degrades to the normal wait instead of
// failing the acquisition.
func TestCollectPurchaseFreeFallsBackToWaiting(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "complete", "license_key": "SHRW1.later",
		})
	}))
	defer srv.Close()

	status, key, err := collectPurchase(context.Background(), client.NewClient(srv.URL),
		client.BuyResult{Free: true, Purchase: "pt_slow"}, &cadence{}, nil)
	if err != nil {
		t.Fatalf("collectPurchase: %v", err)
	}
	if status != client.PurchaseComplete || key != "SHRW1.later" {
		t.Fatalf("status=%q key=%q, want the wait to have picked it up", status, key)
	}
	if calls < 2 {
		t.Fatalf("made %d requests, want the fallback wait to have run", calls)
	}
}
