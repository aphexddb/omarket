package client_test

import (
	"strings"
	"testing"

	"github.com/aphexddb/omarket/client"
)

func TestValidateAppID(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid simple", "hello-shareware", false},
		{"valid min length (3 chars)", "abc", false},
		{"valid max length (64 chars)", strings.Repeat("a", 64), false},
		{"valid digits and hyphens", "app-42-beta", false},
		{"too short (2 chars)", "ab", true},
		{"too long (65 chars)", strings.Repeat("a", 65), true},
		{"uppercase", "Hello-App", true},
		{"underscore", "hello_app", true},
		{"leading hyphen", "-hello", true},
		{"trailing hyphen", "hello-", true},
		{"empty", "", true},
		{"spaces", "hello app", true},
		{"dot", "hello.app", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := client.ValidateAppID(tc.id)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateAppID(%q) err = %v, wantErr %v", tc.id, err, tc.wantErr)
			}
		})
	}
}
