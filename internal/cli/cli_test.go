package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionDoesNotNeedController(t *testing.T) {
	var out strings.Builder
	if err := Run([]string{"version"}, Options{Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "transitforge") || !strings.Contains(got, "dev") {
		t.Fatalf("version output %q", got)
	}
}

func TestApplyEnvReadsTokenFileAndInsecure(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "bootstrap.token")
	if err := os.WriteFile(tok, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRANSITFORGE_CONTROLLER", "https://controller:8443")
	t.Setenv("TRANSITFORGE_TOKEN_FILE", tok)
	t.Setenv("TRANSITFORGE_TOKEN", "")
	t.Setenv("TRANSITFORGE_INSECURE", "true")
	opt, err := applyEnv(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Controller != "https://controller:8443" {
		t.Fatalf("controller %q", opt.Controller)
	}
	if opt.Token != "secret-token" {
		t.Fatalf("token %q", opt.Token)
	}
	if !opt.Insecure {
		t.Fatal("expected insecure from env")
	}
}

func TestApplyEnvFlagsWinOverEnv(t *testing.T) {
	t.Setenv("TRANSITFORGE_CONTROLLER", "https://from-env:8443")
	t.Setenv("TRANSITFORGE_INSECURE", "false")
	opt, err := applyEnv(Options{Controller: "https://from-flag:8443", Insecure: true, Token: "flag-token"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Controller != "https://from-flag:8443" {
		t.Fatalf("controller %q", opt.Controller)
	}
	if opt.Token != "flag-token" {
		t.Fatalf("token %q", opt.Token)
	}
	if !opt.Insecure {
		t.Fatal("flag --insecure must stick")
	}
}
