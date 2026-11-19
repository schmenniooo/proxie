package cli

import (
	"fmt"
	"runtime/debug"
)

// Set at build time, for example:
//
//	go build -ldflags "-X github.com/schmenniooo/proxie/internal/cli.version=1.2.0 -X github.com/schmenniooo/proxie/internal/cli.commit=abc123"
var (
	version = "dev"
	commit  = ""
)

// VersionCmd prints the version and commit.
type VersionCmd struct{}

// Run prints "proxie <version> (commit <commit>)".
func (VersionCmd) Run(s *streams) error {
	_, err := fmt.Fprintf(s.stdout, "proxie %s (commit %s)\n", version, buildCommit())
	return err
}

// buildCommit returns the commit set via ldflags, else the one Go recorded
// when building from a git checkout, else "unknown".
func buildCommit() string {
	if commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
