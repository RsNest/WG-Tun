package cmdexec

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FakeCommandRunner records calls and dispatches to a handler (tests only).
type FakeCommandRunner struct {
	mu       sync.Mutex
	Calls    []Invocation
	Handler  func(stdin []byte, executable string, args []string) (stdout, stderr []byte, err error)
	FailNext string
}

type Invocation struct {
	Executable string
	Args       []string
	Stdin      []byte
}

func (f *FakeCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, []byte, error) {
	return f.RunStdin(ctx, nil, executable, args...)
}

func (f *FakeCommandRunner) RunStdin(ctx context.Context, stdin []byte, executable string, args ...string) ([]byte, []byte, error) {
	if err := ValidateExecutable(executable); err != nil {
		return nil, nil, err
	}
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
	}
	f.mu.Lock()
	f.Calls = append(f.Calls, Invocation{Executable: executable, Args: append([]string{}, args...), Stdin: append([]byte{}, stdin...)})
	fail := f.FailNext
	if fail != "" {
		f.FailNext = ""
	}
	h := f.Handler
	f.mu.Unlock()
	joined := executable + " " + strings.Join(args, " ")
	if fail != "" && strings.Contains(joined, fail) {
		return nil, []byte("injected failure"), fmt.Errorf("injected failure: %s", fail)
	}
	if h == nil {
		return nil, nil, fmt.Errorf("FakeCommandRunner has no handler for %s", joined)
	}
	return h(stdin, executable, args)
}

func (f *FakeCommandRunner) CallCount(executable string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Executable == executable {
			n++
		}
	}
	return n
}
