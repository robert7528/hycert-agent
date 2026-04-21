package executor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const DefaultTimeout = 30 * time.Second

// Run executes a shell command with the default 30s timeout.
func Run(ctx context.Context, command string) (string, error) {
	return RunWithTimeout(ctx, command, DefaultTimeout)
}

// RunWithTimeout executes a shell command with a caller-specified timeout.
// On Linux/macOS: sh -c "cmd"
// On Windows: powershell -NoProfile -Command "cmd"
//
// Deployers should pass a timeout appropriate for the service being
// reloaded — most reloads are near-instant (nginx -s reload, systemctl
// reload), but `Restart-Service Tomcat` or `iisreset /restart` can block
// for minutes during JVM / app-pool spin-up.
func RunWithTimeout(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if command == "" {
		return "", nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Force UTF-8 so Go doesn't misread CP950/GBK console code pages as UTF-8.
		// $OutputEncoding controls downstream pipe; [Console]::OutputEncoding
		// controls host stdout — both must be set or cmdlet warnings (e.g.
		// Restart-Service) still come through as mojibake.
		wrapped := "$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", wrapped)
	default:
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))

	if err != nil {
		return out, fmt.Errorf("execute %q: %w (output: %s)", command, err, out)
	}
	return out, nil
}

// ReloadTimeoutFor returns how long to wait for the reload *command* to
// return (not how long to wait for the service to finish warming up —
// that's the verify timeout, handled separately).
//
//   - nginx / apache / haproxy / hyproxy : 30s
//     (nginx -s reload, systemctl reload — near-instant in normal ops)
//   - tomcat / iis : 60s
//     (Restart-Service / iisreset triggers; full cold-start wait is
//     covered by verify timeout afterwards, not here)
//   - kubernetes : 60s
//     (kubectl apply + API server roundtrip; kubectl has its own -timeout
//     but we wrap in a shell so we control the ceiling)
//
// Callers can override via target_detail.reload_timeout (seconds).
func ReloadTimeoutFor(service string, overrideSeconds int) time.Duration {
	if overrideSeconds > 0 {
		return time.Duration(overrideSeconds) * time.Second
	}
	switch service {
	case "tomcat", "iis", "kubernetes":
		return 60 * time.Second
	default:
		return 30 * time.Second
	}
}
