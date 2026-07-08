// Package version holds build-time version information.
//
// The exported variables are populated at build time via
// -ldflags "-X github.com/HeoJeongBo/weft/internal/version.Version=...".
package version

import (
	"fmt"
	"runtime"
)

// Build metadata, overridden at release time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a one-line, human-readable version summary.
func String() string {
	return fmt.Sprintf("weft %s (commit %s, built %s, %s %s/%s)",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
