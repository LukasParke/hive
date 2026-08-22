package hostmetrics

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

func TestNewExecutor(t *testing.T) {
	dir := t.TempDir()
	e := NewExecutor(dir, false)
	if e.enabled || e.distro != "unknown" {
		t.Errorf("unexpected executor: %+v", e)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte("ID=ubuntu\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e = NewExecutor(root, true)
	if !e.enabled || e.distro != "ubuntu" {
		t.Errorf("unexpected executor: %+v", e)
	}
}

func TestBuildCommandTable(t *testing.T) {
	distroCmd := map[string]map[agentv1.HostOperation][]string{
		"ubuntu": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK:     {"apt-get", "update"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL:      {"apt-get", "upgrade", "-y"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY: {"apt-get", "upgrade", "-y", "--only-upgrade"},
			agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE:          {"shutdown", "-r", "+5"},
			agentv1.HostOperation_HOST_OPERATION_REBOOT_CANCEL:            {"shutdown", "-c"},
			agentv1.HostOperation_HOST_OPERATION_SYSTEMCTL_STATUS:         {"systemctl", "status", "nginx"},
			agentv1.HostOperation_HOST_OPERATION_JOURNALCTL_TAIL:          {"journalctl", "-u", "nginx", "-n", "50", "--no-pager"},
		},
		"debian": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK: {"apt-get", "update"},
		},
		"centos": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK:     {"yum", "check-update"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL:      {"yum", "update", "-y"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY: {"yum", "update", "--security", "-y"},
		},
		"rocky": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK: {"yum", "check-update"},
		},
		"alma": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK: {"yum", "check-update"},
		},
		"rhel": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK: {"yum", "check-update"},
		},
		"fedora": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK:     {"dnf", "check-update"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL:      {"dnf", "update", "-y"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY: {"dnf", "update", "--security", "-y"},
		},
		"alpine": {
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK: {"apk", "update"},
			agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL:  {"apk", "upgrade"},
		},
	}

	for distro, ops := range distroCmd {
		e := &Executor{enabled: true, distro: distro}
		for op, want := range ops {
			got, timeout, err := e.buildCommand(&agentv1.HostOperationRequest{
				Operation: op,
				Params:    map[string]string{"minutes": "5", "unit": "nginx", "lines": "50"},
			})
			if err != nil {
				t.Fatalf("%s/%v: unexpected error: %v", distro, op, err)
			}
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("%s/%v: cmd = %v, want %v", distro, op, got, want)
			}
			if timeout <= 0 {
				t.Errorf("%s/%v: timeout must be positive, got %v", distro, op, timeout)
			}
		}
	}
}

func TestBuildCommandTimeouts(t *testing.T) {
	e := &Executor{enabled: true, distro: "ubuntu"}

	if _, got, err := e.buildCommand(&agentv1.HostOperationRequest{Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK}); err != nil || got != 2*time.Minute {
		t.Errorf("update-check timeout = %v, err %v; want 2m", got, err)
	}
	if _, got, err := e.buildCommand(&agentv1.HostOperationRequest{Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL}); err != nil || got != 10*time.Minute {
		t.Errorf("upgrade-all timeout = %v, err %v; want 10m", got, err)
	}
	if _, got, err := e.buildCommand(&agentv1.HostOperationRequest{Operation: agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE}); err != nil || got != defaultTimeout {
		t.Errorf("reboot timeout = %v, err %v; want default", got, err)
	}
}

func TestBuildCommandDefaultsAndValidation(t *testing.T) {
	e := &Executor{enabled: true, distro: "ubuntu"}
	op := agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE

	// Missing params map and empty minutes both default to +1.
	cmd, _, err := e.buildCommand(&agentv1.HostOperationRequest{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cmd, " ") != "shutdown -r +1" {
		t.Errorf("default reboot minutes = %v", cmd)
	}

	cmd, _, err = e.buildCommand(&agentv1.HostOperationRequest{
		Operation: op,
		Params:    map[string]string{"minutes": ""},
	})
	if err != nil || strings.Join(cmd, " ") != "shutdown -r +1" {
		t.Errorf("empty reboot minutes should default to 1, got %v err %v", cmd, err)
	}

	statusOp := agentv1.HostOperation_HOST_OPERATION_SYSTEMCTL_STATUS
	for name, tc := range map[string]struct {
		unit    string
		wantErr string
	}{
		"missing unit":   {"", "missing 'unit' parameter"},
		"injection unit": {"nginx; rm -rf /", "invalid unit name"},
		"backtick unit":  {"nginx`id`", "invalid unit name"},
		"dollar unit":    {"$(reboot)", "invalid unit name"},
	} {
		_, _, err := e.buildCommand(&agentv1.HostOperationRequest{
			Operation: statusOp,
			Params:    map[string]string{"unit": tc.unit},
		})
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}

	journalOp := agentv1.HostOperation_HOST_OPERATION_JOURNALCTL_TAIL
	_, _, err = e.buildCommand(&agentv1.HostOperationRequest{Operation: journalOp, Params: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "missing 'unit'") {
		t.Errorf("journalctl missing unit: err = %v", err)
	}
	_, _, err = e.buildCommand(&agentv1.HostOperationRequest{
		Operation: journalOp,
		Params:    map[string]string{"unit": "x|y"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid unit name") {
		t.Errorf("journalctl injection unit: err = %v", err)
	}
	cmd, _, err = e.buildCommand(&agentv1.HostOperationRequest{
		Operation: journalOp,
		Params:    map[string]string{"unit": "cron"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cmd, " ") != "journalctl -u cron -n 100 --no-pager" {
		t.Errorf("journalctl default lines = %v", cmd)
	}
}

func TestBuildCommandUnsupported(t *testing.T) {
	e := &Executor{enabled: true, distro: "plan9"} // no package manager matches
	_, _, err := e.buildCommand(&agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatalf("err = %v, want unsupported operation", err)
	}

	e.distro = "ubuntu"
	_, _, err = e.buildCommand(&agentv1.HostOperationRequest{Operation: agentv1.HostOperation_HOST_OPERATION_UNSPECIFIED})
	if err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatalf("err = %v, want unsupported operation for unspecified op", err)
	}
}

func TestExecuteDisabled(t *testing.T) {
	e := &Executor{enabled: false}
	_, err := e.Execute(context.Background(), &agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_REBOOT_CANCEL,
	})
	if err == nil || !strings.Contains(err.Error(), "host management is not enabled") {
		t.Fatalf("err = %v, want disabled error", err)
	}
}

func TestExecuteUnsupportedBeforeSpawn(t *testing.T) {
	e := &Executor{enabled: true, distro: "plan9"}
	_, err := e.Execute(context.Background(), &agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatalf("err = %v, want unsupported operation", err)
	}
}

func TestExecuteRunsAllowlistedCommand(t *testing.T) {
	if _, err := exec.LookPath("nsenter"); err != nil {
		t.Skip("nsenter not available on this host")
	}
	e := &Executor{enabled: true, distro: "ubuntu"}
	resp, err := e.Execute(context.Background(), &agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_JOURNALCTL_TAIL,
		Params:    map[string]string{"unit": "nonexistent-unit-xyz", "lines": "3"},
	})
	if err != nil {
		// nsenter may be unable to enter namespace 1 without privileges;
		// that failure surfaces as a non-ExitError exec failure.
		if strings.Contains(err.Error(), "exec failed") {
			return
		}
		t.Fatalf("unexpected error: %v", err)
	}
	// Without root the namespaced command typically fails with a nonzero code.
	if resp.ExitCode == 0 && resp.Stdout == "" {
		t.Log("command unexpectedly succeeded without output")
	}
	if resp.DurationMs < 0 {
		t.Errorf("DurationMs must be non-negative, got %d", resp.DurationMs)
	}
}
