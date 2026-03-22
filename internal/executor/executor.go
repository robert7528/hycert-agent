package executor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Run executes a shell command with OS-aware shell selection.
// On Linux/macOS: sh -c "cmd"
// On Windows: powershell -NoProfile -Command "cmd"
func Run(ctx context.Context, command string) (string, error) {
	if command == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
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
