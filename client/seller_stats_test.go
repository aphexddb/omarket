package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aphexddb/omarket/client"
)

// TestGetSellerStats checks the happy path: the seller token goes out as a
// bearer header, and every field of the response reaches the caller.
func TestGetSellerStats(t *testing.T) {
	var gotAuth, gotPath string
	c := serving(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"seller_id": "sel_abc",
			"apps": [
				{"id":"hello-tool","name":"Hello Tool","price_usd_cents":900,"ware":"shareware","listed":true,"licenses":12,"gross_usd_cents":10800},
				{"id":"postcard-cli","name":"Postcard CLI","price_usd_cents":0,"ware":"postcardware","listed":false,"licenses":3,"gross_usd_cents":0}
			],
			"total_licenses": 15,
			"total_gross_usd_cents": 10800
		}`))
	})

	got, err := c.GetSellerStats(context.Background(), "st_secret")
	if err != nil {
		t.Fatalf("GetSellerStats: %v", err)
	}
	if gotAuth != "Bearer st_secret" {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}
	if gotPath != "/api/sellers/stats" {
		t.Errorf("path = %q, want /api/sellers/stats", gotPath)
	}
	if got.SellerID != "sel_abc" {
		t.Errorf("SellerID = %q, want sel_abc", got.SellerID)
	}
	if len(got.Apps) != 2 {
		t.Fatalf("len(Apps) = %d, want 2", len(got.Apps))
	}
	first := got.Apps[0]
	if first.ID != "hello-tool" || first.Name != "Hello Tool" || first.PriceUSDCents != 900 ||
		first.Ware != "shareware" || !first.Listed || first.Licenses != 12 || first.GrossUSDCents != 10800 {
		t.Errorf("Apps[0] = %+v", first)
	}
	second := got.Apps[1]
	if second.PriceUSDCents != 0 || second.Ware != "postcardware" || second.Listed || second.Licenses != 3 {
		t.Errorf("Apps[1] = %+v", second)
	}
	if got.TotalLicenses != 15 || got.TotalGrossUSDCents != 10800 {
		t.Errorf("totals = %d/%d, want 15/10800", got.TotalLicenses, got.TotalGrossUSDCents)
	}
}

// TestGetSellerStatsEmpty checks a seller with no apps gets an empty slice
// rather than nil, so rendering can range over it unguarded.
func TestGetSellerStatsEmpty(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "application/json",
		`{"seller_id":"sel_abc","apps":null,"total_licenses":0,"total_gross_usd_cents":0}`))

	got, err := c.GetSellerStats(context.Background(), "st_secret")
	if err != nil {
		t.Fatalf("GetSellerStats: %v", err)
	}
	if got.Apps == nil {
		t.Fatal("Apps = nil, want an empty slice")
	}
	if len(got.Apps) != 0 {
		t.Fatalf("len(Apps) = %d, want 0", len(got.Apps))
	}
}

// TestGetSellerStatsUnauthorized checks a rejected token surfaces as a
// typed 401 so the command can tell the seller to run `sell init`.
func TestGetSellerStatsUnauthorized(t *testing.T) {
	c := serving(t, respond(http.StatusUnauthorized, "application/json", `{"error":"invalid seller token"}`))

	_, err := c.GetSellerStats(context.Background(), "st_wrong")
	var herr *client.HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v (%T), want *client.HTTPError", err, err)
	}
	if herr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", herr.StatusCode)
	}
}

// TestGetSellerStatsGarbageBody checks an undecodable body is an error, not
// a silently empty stats table that would read as "you've sold nothing".
func TestGetSellerStatsGarbageBody(t *testing.T) {
	c := serving(t, respond(http.StatusOK, "text/html", `<html>maintenance</html>`))

	if _, err := c.GetSellerStats(context.Background(), "st_secret"); err == nil {
		t.Fatal("expected an error for an undecodable stats body")
	}
}
