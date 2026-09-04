package cmdexec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// OSCommandRunner is the production runner. It never invokes a shell.
type OSCommandRunner struct {
	Timeout time.Duration
}

func (r OSCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, []byte, error) {
	return r.RunStdin(ctx, nil, executable, args...)
}

func (r OSCommandRunner) RunStdin(ctx context.Context, stdin []byte, executable string, args ...string) ([]byte, []byte, error) {
	if err := ValidateExecutable(executable); err != nil {
		return nil, nil, err
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out, errb := stdout.Bytes(), stderr.Bytes()
	if err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return out, errb, &ExitError{Executable: executable, Args: args, Code: code, Stderr: string(errb)}
	}
	return out, errb, nil
}
