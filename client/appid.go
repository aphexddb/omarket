package client

import (
	"fmt"
	"regexp"
)

var appIDPattern = regexp.MustCompile(`^[a-z0-9-]{3,64}$`)

// ValidateAppID checks id against the app-id rule shared with the server:
// 3-64 characters of lowercase letters, digits, and hyphens, with no
// leading or trailing hyphen. It's a local, fast-fail check only; the
// server additionally enforces a reserved-names list this function does
// not know about.
func ValidateAppID(id string) error {
	if !appIDPattern.MatchString(id) {
		return fmt.Errorf("invalid app id %q: must be 3-64 characters, lowercase letters, digits, and hyphens only", id)
	}
	if id[0] == '-' || id[len(id)-1] == '-' {
		return fmt.Errorf("invalid app id %q: must not start or end with a hyphen", id)
	}
	return nil
}
