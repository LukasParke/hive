// Package version provides build-time version information and comparison utilities.
package version

import (
	"fmt"
	"strings"
	"time"
)

// Current is injected at build time via ldflags.
var Current = "dev"

// Info holds version metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildTime string `json:"buildTime,omitempty"`
}

// Get returns the current version info.
func Get() Info {
	return Info{
		Version:   Current,
		BuildTime: time.Now().UTC().Format(time.RFC3339),
	}
}

// IsDev returns true if the current build is a development build.
func IsDev() bool {
	return Current == "dev" || Current == ""
}

// Compare compares two version strings. It returns:
//   -1 if a < b
//    0 if a == b
//    1 if a > b
//
// This handles semver and nightly tags (e.g. "nightly-20250603").
func Compare(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	if a == b {
		return 0
	}

	if strings.Contains(a, ".") && strings.Contains(b, ".") {
		return compareSemver(a, b)
	}

	if a < b {
		return -1
	}
	return 1
}

func compareSemver(a, b string) int {
	aCore, aPre, _ := splitSemver(a)
	bCore, bPre, _ := splitSemver(b)

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	for i := 0; i < max(len(aParts), len(bParts)); i++ {
		var av, bv int
		if i < len(aParts) {
			_, _ = fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			_, _ = fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}

	if aPre == "" && bPre != "" {
		return 1
	}
	if aPre != "" && bPre == "" {
		return -1
	}
	if aPre < bPre {
		return -1
	}
	if aPre > bPre {
		return 1
	}
	return 0
}

func splitSemver(s string) (core, pre, build string) {
	if i := strings.Index(s, "+"); i >= 0 {
		build = s[i+1:]
		s = s[:i]
	}
	if i := strings.Index(s, "-"); i >= 0 {
		pre = s[i+1:]
		core = s[:i]
		return
	}
	core = s
	return
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
