package secret

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// opRead shells out to `op read <ref>` (1Password CLI).
func opRead(ctx context.Context, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "op", "read", ref)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("op: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("op: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// keychainRead shells out to macOS `security find-generic-password`.
func keychainRead(ctx context.Context, service, account string) (string, error) {
	cmd := exec.CommandContext(ctx, "security", "find-generic-password",
		"-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("keychain: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("keychain: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}
