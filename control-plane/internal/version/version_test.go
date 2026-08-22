package version

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{"identical", "1.2.3", "1.2.3", 0},
		{"identical with v prefix", "v1.2.3", "1.2.3", 0},
		{"both v prefix", "v1.2.3", "v1.2.4", -1},
		{"major bump", "1.0.0", "2.0.0", -1},
		{"minor bump", "1.2.0", "1.10.0", -1},
		{"numeric not lexicographic", "1.9.0", "1.10.0", -1},
		{"patch bump", "1.2.3", "1.2.4", -1},
		{"missing component sorts lower", "1.2", "1.2.0", 0},
		{"missing component lower than patch", "1.2", "1.2.1", -1},
		{"prerelease sorts before release", "1.2.3-rc.1", "1.2.3", -1},
		{"release sorts after prerelease", "1.2.3", "1.2.3-rc.1", 1},
		{"prerelease lexicographic", "1.2.3-alpha", "1.2.3-beta", -1},
		{"prerelease equal", "1.2.3-rc.1", "1.2.3-rc.1", 0},
		{"build metadata ignored for core", "1.2.3+build.1", "1.2.3", 0},
		{"mixed semver and nightly falls to string compare", "1.2.3", "nightly-20250603", -1},
		{"nightly tags lexicographic", "nightly-20250601", "nightly-20250603", -1},
		{"nightly newer", "nightly-20250603", "nightly-20250601", 1},
		{"empty vs semver", "", "0.0.1", -1},
		{"semver vs empty", "0.0.1", "", 1},
		{"dev strings", "dev", "dev", 0},
		{"single component strings", "abc", "abd", -1},
		{"reversed greater", "2.0.0", "1.0.0", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compare(tc.a, tc.b); got != tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			// Comparison must be antisymmetric.
			if got := Compare(tc.b, tc.a); got != -tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
			}
		})
	}
}

func TestCompareSemverDirect(t *testing.T) {
	// Exercise splitSemver's build-metadata branch through compareSemver.
	if got := compareSemver("1.2.3+exp", "1.2.3+sha.5114f85"); got != 0 {
		t.Fatalf("compareSemver build metadata = %d, want 0", got)
	}
	if got := compareSemver("1.2.3-rc.2+exp", "1.2.3-rc.1"); got != 1 {
		t.Fatalf("compareSemver prerelease with build = %d, want 1", got)
	}
}

func TestSplitSemver(t *testing.T) {
	cases := []struct {
		in               string
		core, pre, build string
	}{
		{"1.2.3", "1.2.3", "", ""},
		{"1.2.3-rc.1", "1.2.3", "rc.1", ""},
		{"1.2.3+build.5", "1.2.3", "", "build.5"},
		{"1.2.3-rc.1+build.5", "1.2.3", "rc.1", "build.5"},
	}
	for _, tc := range cases {
		core, pre, build := splitSemver(tc.in)
		if core != tc.core || pre != tc.pre || build != tc.build {
			t.Fatalf("splitSemver(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.in, core, pre, build, tc.core, tc.pre, tc.build)
		}
	}
}

func TestMax(t *testing.T) {
	if got := max(3, 7); got != 7 {
		t.Fatalf("max(3, 7) = %d, want 7", got)
	}
	if got := max(9, 2); got != 9 {
		t.Fatalf("max(9, 2) = %d, want 9", got)
	}
}

func TestGetAndIsDev(t *testing.T) {
	prev := Current
	t.Cleanup(func() { Current = prev })

	Current = "v1.4.2"
	info := Get()
	if info.Version != "v1.4.2" {
		t.Fatalf("Get().Version = %q, want v1.4.2", info.Version)
	}
	if !strings.HasSuffix(info.BuildTime, "Z") {
		t.Fatalf("Get().BuildTime = %q, want RFC3339 UTC", info.BuildTime)
	}
	if _, err := json.Marshal(info); err != nil {
		t.Fatalf("Info must be JSON-marshalable: %v", err)
	}
	if IsDev() {
		t.Fatal("IsDev() = true for release version")
	}

	Current = "dev"
	if !IsDev() {
		t.Fatal("IsDev() = false for dev version")
	}

	Current = ""
	if !IsDev() {
		t.Fatal("IsDev() = false for empty version")
	}
}
