// Package version holds the build identity injected by GoReleaser via
// -ldflags. Local `go build` leaves Version as "dev".
package version

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single-line identity, e.g. "0.1.0 (abc1234, 2026-08-25)".
func String() string {
	return fmt.Sprintf("%s (%s, %s)", Version, Commit, Date)
}
