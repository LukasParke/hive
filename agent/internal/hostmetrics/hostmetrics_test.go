package hostmetrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/common"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector("", false)
	if c.hostRoot != "" || c.hostMgmtEnabled {
		t.Errorf("unexpected collector state: %+v", c)
	}
	if c.HostMgmtEnabled() {
		t.Error("host mgmt should be disabled")
	}

	// Save and restore gopsutil's HOST_* environment around the collector,
	// which mutates it when hostRoot is set.
	saved := map[string]string{}
	for _, key := range []string{"HOST_PROC", "HOST_SYS", "HOST_ETC", "HOST_VAR", "HOST_RUN"} {
		saved[key] = os.Getenv(key)
	}
	defer func() {
		for key, v := range saved {
			if v == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, v)
			}
		}
	}()

	c2 := NewCollector("", true)
	if !c2.HostMgmtEnabled() {
		t.Error("host mgmt should be enabled")
	}

	c2 = NewCollector("/host", true)
	if got := os.Getenv("HOST_PROC"); got != "/host/proc" {
		t.Errorf("HOST_PROC = %q, want /host/proc", got)
	}
	if c2 == nil {
		t.Fatal("collector must not be nil")
	}
}

func TestCollectPopulatesCache(t *testing.T) {
	c := NewCollector("", false)
	if m := c.Metrics(); m != nil {
		t.Fatal("cache should start empty")
	}
	c.collect(context.Background())

	m := c.Metrics()
	if m == nil {
		t.Fatal("expected cached metrics after collect")
	}
	if m.CollectedAt <= 0 || m.CollectedAt > time.Now().Unix()+5 {
		t.Errorf("implausible CollectedAt: %d", m.CollectedAt)
	}
}

func TestRunThenCancel(t *testing.T) {
	c := NewCollector("", false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.Metrics() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if c.Metrics() == nil {
		t.Fatal("expected metrics collected by Run")
	}
}

func TestRefreshPackageStatus(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "os-release"), []byte("ID=alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewCollector(dir, false)
	if s := c.PackageStatus(); s != nil {
		t.Fatal("package cache should start empty")
	}
	c.RefreshPackageStatus(context.Background())
	s := c.PackageStatus()
	if s == nil {
		t.Fatal("expected cached package status")
	}
	if s.PackageManager != "apk" {
		t.Errorf("PackageManager = %q, want apk", s.PackageManager)
	}
}

func TestDetectDistro(t *testing.T) {
	dir := t.TempDir()

	if got := detectDistro(dir); got != "unknown" {
		t.Errorf("missing os-release should be unknown, got %q", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "etc", "os-release"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("PRETTY_NAME=Whatever\nID=\"ubuntu\"\nVERSION=1\n")
	if got := detectDistro(dir); got != "ubuntu" {
		t.Errorf("got %q, want ubuntu", got)
	}

	write("ID=Fedora\n")
	if got := detectDistro(dir); got != "fedora" {
		t.Errorf("quoted/lowercase handling failed: %q", got)
	}

	write("NAME=NoIDHere\n")
	if got := detectDistro(dir); got != "unknown" {
		t.Errorf("os-release without ID= should be unknown, got %q", got)
	}
}

func TestCollectPackageStatusDispatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	osRelease := filepath.Join(root, "etc", "os-release")

	set := func(id string) {
		t.Helper()
		if err := os.WriteFile(osRelease, []byte("ID="+id+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	checks := map[string]string{
		"debian": "apt",
		"rocky":  "yum",
		"alpine": "apk",
		"sunos":  "unknown",
	}
	for id, want := range checks {
		set(id)
		got := collectPackageStatus(root)
		if got.PackageManager != want {
			t.Errorf("distro %q -> PackageManager %q, want %q", id, got.PackageManager, want)
		}
		if got.LastCheckedAt <= 0 {
			t.Errorf("distro %q -> LastCheckedAt not set", id)
		}
	}
}

func TestCollectAptStatus(t *testing.T) {
	root := t.TempDir()
	dpkgDir := filepath.Join(root, "var", "lib", "dpkg")
	listsDir := filepath.Join(root, "var", "lib", "apt", "lists")
	for _, d := range []string{dpkgDir, listsDir, filepath.Join(root, "var", "run")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	status := "Package: a\nStatus: install ok installed\n\nPackage: b\nStatus: deinstall ok config-files\n\nPackage: c\nStatus: install ok installed\n"
	if err := os.WriteFile(filepath.Join(dpkgDir, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var", "run"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "var", "run", "reboot-required"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(listsDir, "archive.ubuntu.com_Packages"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := collectAptStatus(root)
	if resp.TotalInstalled != 2 {
		t.Errorf("TotalInstalled = %d, want 2", resp.TotalInstalled)
	}
	if !resp.RebootRequired {
		t.Error("RebootRequired should be true")
	}
	if resp.UpgradableCount != 0 {
		t.Errorf("UpgradableCount = %d, want 0 (parser returns no updates)", resp.UpgradableCount)
	}
}

func TestCollectYumStatus(t *testing.T) {
	root := t.TempDir()
	resp := collectYumStatus(root)
	if resp.TotalInstalled != 0 {
		t.Errorf("no rpm db -> TotalInstalled 0, got %d", resp.TotalInstalled)
	}
	if resp.PackageManager != "yum" {
		t.Errorf("PackageManager = %q", resp.PackageManager)
	}

	rpmRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rpmRoot, "var", "lib", "rpm"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rpmRoot, "usr", "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // G306: executable fixture needs +x
	if err := os.WriteFile(filepath.Join(rpmRoot, "usr", "bin", "dnf"), []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	resp = collectYumStatus(rpmRoot)
	if resp.TotalInstalled != -1 {
		t.Errorf("rpm db present -> TotalInstalled -1, got %d", resp.TotalInstalled)
	}
	if resp.PackageManager != "dnf" {
		t.Errorf("dnf binary present -> PackageManager dnf, got %q", resp.PackageManager)
	}
}

func TestCollectApkStatus(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "lib", "apk", "db")
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		t.Fatal(err)
	}
	installed := "P:alpine-baselayout\nP:busybox\nT:not-a-package\nP:musl\n"
	if err := os.WriteFile(filepath.Join(dbDir, "installed"), []byte(installed), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := collectApkStatus(root)
	if resp.TotalInstalled != 3 {
		t.Errorf("TotalInstalled = %d, want 3", resp.TotalInstalled)
	}

	emptyRoot := t.TempDir()
	if resp := collectApkStatus(emptyRoot); resp.TotalInstalled != 0 {
		t.Errorf("missing db -> TotalInstalled 0, got %d", resp.TotalInstalled)
	}
}

// TestCollectWithBrokenHostProc points gopsutil at a stub /proc so every
// source read fails except partition listing, exercising the error branches
// of Collector.collect without touching the real filesystem.
func TestCollectWithBrokenHostProc(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "proc")
	if err := os.MkdirAll(procDir, 0o750); err != nil {
		t.Fatal(err)
	}
	mounts := "none /definitely/not/mounted ext4 rw 0 0\n"
	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(mounts), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{
		common.HostProcEnvKey: procDir,
	})

	c := NewCollector("", false)
	c.collect(ctx)

	m := c.Metrics()
	if m == nil {
		t.Fatal("collect must still cache a snapshot when sources fail")
	}
	if m.CollectedAt <= 0 {
		t.Error("snapshot timestamp missing")
	}
	if len(m.Filesystems) != 0 {
		t.Errorf("unusable mountpoint should be skipped, got %d entries", len(m.Filesystems))
	}
}
