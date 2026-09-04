package cmdexec_test

import (
	"context"
	"testing"

	"transitforge/internal/cmdexec"
)

func TestRejectsShell(t *testing.T) {
	var r cmdexec.OSCommandRunner
	if _, _, err := r.Run(context.Background(), "sh", "-c", "iptables -F"); err == nil {
		t.Fatal("shell must be rejected")
	}
	if err := cmdexec.ValidateExecutable("iptables"); err != nil {
		t.Fatal(err)
	}
}
