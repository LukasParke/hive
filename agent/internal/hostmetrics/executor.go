package hostmetrics

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

const defaultTimeout = 30 * time.Second

// Executor runs host-level operations via nsenter.
type Executor struct {
	enabled bool
	distro  string
}

// NewExecutor creates a host operation executor.
func NewExecutor(hostRoot string, enabled bool) *Executor {
	return &Executor{
		enabled: enabled,
		distro:  detectDistro(hostRoot),
	}
}

// Execute runs a host operation and returns the result.
func (e *Executor) Execute(ctx context.Context, req *agentv1.HostOperationRequest) (*agentv1.HostOperationResponse, error) {
	if !e.enabled {
		return nil, fmt.Errorf("host management is not enabled")
	}

	cmd, timeout, err := e.buildCommand(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	c := exec.CommandContext(ctx, "nsenter", append([]string{"-t", "1", "-m", "-u", "-i", "-n", "--"}, cmd...)...)

	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr

	err = c.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec failed: %w", err)
		}
	}

	return &agentv1.HostOperationResponse{
		ExitCode:   int32(exitCode),
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: duration,
	}, nil
}

func (e *Executor) buildCommand(req *agentv1.HostOperationRequest) ([]string, time.Duration, error) {
	params := req.Params
	if params == nil {
		params = make(map[string]string)
	}

	switch req.Operation {
	case agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK:
		switch e.distro {
		case "ubuntu", "debian":
			return []string{"apt-get", "update"}, 2 * time.Minute, nil
		case "centos", "rhel", "rocky", "alma":
			return []string{"yum", "check-update"}, 2 * time.Minute, nil
		case "fedora":
			return []string{"dnf", "check-update"}, 2 * time.Minute, nil
		case "alpine":
			return []string{"apk", "update"}, time.Minute, nil
		}

	case agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL:
		switch e.distro {
		case "ubuntu", "debian":
			return []string{"apt-get", "upgrade", "-y"}, 10 * time.Minute, nil
		case "centos", "rhel", "rocky", "alma":
			return []string{"yum", "update", "-y"}, 10 * time.Minute, nil
		case "fedora":
			return []string{"dnf", "update", "-y"}, 10 * time.Minute, nil
		case "alpine":
			return []string{"apk", "upgrade"}, 5 * time.Minute, nil
		}

	case agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY:
		switch e.distro {
		case "ubuntu", "debian":
			return []string{"apt-get", "upgrade", "-y", "--only-upgrade"}, 10 * time.Minute, nil
		case "centos", "rhel", "rocky", "alma":
			return []string{"yum", "update", "--security", "-y"}, 10 * time.Minute, nil
		case "fedora":
			return []string{"dnf", "update", "--security", "-y"}, 10 * time.Minute, nil
		}

	case agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE:
		minutes := params["minutes"]
		if minutes == "" {
			minutes = "1"
		}
		return []string{"shutdown", "-r", "+" + minutes}, defaultTimeout, nil

	case agentv1.HostOperation_HOST_OPERATION_REBOOT_CANCEL:
		return []string{"shutdown", "-c"}, defaultTimeout, nil

	case agentv1.HostOperation_HOST_OPERATION_SYSTEMCTL_STATUS:
		unit := params["unit"]
		if unit == "" {
			return nil, 0, fmt.Errorf("missing 'unit' parameter")
		}
		// Basic validation: prevent injection
		if strings.ContainsAny(unit, ";&|`$(){}") {
			return nil, 0, fmt.Errorf("invalid unit name: %q", unit)
		}
		return []string{"systemctl", "status", unit}, defaultTimeout, nil

	case agentv1.HostOperation_HOST_OPERATION_JOURNALCTL_TAIL:
		unit := params["unit"]
		if unit == "" {
			return nil, 0, fmt.Errorf("missing 'unit' parameter")
		}
		if strings.ContainsAny(unit, ";&|`$(){}") {
			return nil, 0, fmt.Errorf("invalid unit name: %q", unit)
		}
		lines := params["lines"]
		if lines == "" {
			lines = "100"
		}
		return []string{"journalctl", "-u", unit, "-n", lines, "--no-pager"}, defaultTimeout, nil
	}

	return nil, 0, fmt.Errorf("unsupported operation %v for distro %q", req.Operation, e.distro)
}
