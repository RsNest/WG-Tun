package cmdexec

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// CommandRunner executes host commands as argument arrays (never a shell string).
type CommandRunner interface {
	Run(ctx context.Context, executable string, args ...string) (stdout, stderr []byte, err error)
	RunStdin(ctx context.Context, stdin []byte, executable string, args ...string) (stdout, stderr []byte, err error)
}

var allowed = map[string]bool{
	"iptables":         true,
	"iptables-save":    true,
	"iptables-restore": true,
	"ip":               true,
	"wg":               true,
	"haproxy":          true,
	"systemctl":        true,
	"sysctl":           true,
	"ping":             true,
	"ss":               true,
	"flock":            true,
	"install":          true,
}

func ValidateExecutable(executable string) error {
	if executable == "" {
		return fmt.Errorf("empty executable")
	}
	if strings.ContainsAny(executable, " \t\n;&|$`<>") || strings.Contains(executable, "\x00") {
		return fmt.Errorf("illegal executable name")
	}
	base := filepath.Base(executable)
	if !allowed[base] {
		return fmt.Errorf("executable %q is not in the proxyctl allowlist", base)
	}
	return nil
}

type ExitError struct {
	Executable string
	Args       []string
	Code       int
	Stderr     string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s: exit %d: %s", e.Executable, e.Code, strings.TrimSpace(e.Stderr))
}
