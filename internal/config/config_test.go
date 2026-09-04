package config_test

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"transitforge/internal/config"
)

func TestLoadControllerYAML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("listen: \"127.0.0.1:9443\"\ndata_dir: \"./data\"\ntls:\n  required: false\nauth:\n  max_skew: 2m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadController(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9443" {
		t.Fatalf("listen %s", cfg.Listen)
	}
	if cfg.Auth.MaxSkew.Duration() != 2*time.Minute {
		t.Fatalf("skew %s", cfg.Auth.MaxSkew.Duration())
	}
}

func TestAgentRequiresFields(t *testing.T) {
	_, err := config.LoadAgent(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("missing file")
	}
}

func TestEnsureSelfSignedIncludesDNSNames(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "server.crt")
	key := filepath.Join(dir, "server.key")
	if err := config.EnsureSelfSigned(cert, key, "0.0.0.0:8443", []string{"controller"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("no pem")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	wantDNS := map[string]bool{"localhost": false, "controller": false}
	for _, n := range c.DNSNames {
		if _, ok := wantDNS[n]; ok {
			wantDNS[n] = true
		}
	}
	for n, ok := range wantDNS {
		if !ok {
			t.Fatalf("missing SAN DNS %s in %v", n, c.DNSNames)
		}
	}
	hasLoopback := false
	for _, ip := range c.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Fatalf("missing 127.0.0.1 SAN in %v", c.IPAddresses)
	}
}

func TestAgentHaproxyReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.yaml")
	if err := os.WriteFile(p, []byte("node_name: n\ncontroller_url: https://127.0.0.1:8443\ntoken_file: /t\nhaproxy_reload: external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAgent(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HaproxyReload != "external" {
		t.Fatalf("got %q", cfg.HaproxyReload)
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("node_name: n\ncontroller_url: https://127.0.0.1:8443\ntoken_file: /t\nhaproxy_reload: dbus\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadAgent(bad); err == nil {
		t.Fatal("expected invalid haproxy_reload")
	}
}

func TestResolveDBPathRenamesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "proxyctl.db")
	if err := os.WriteFile(old, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := config.ResolveDBPath(dir)
	want := filepath.Join(dir, "transitforge.db")
	if got != want {
		t.Fatalf("path %s", got)
	}
	if !fileExistsForTest(want) {
		t.Fatal("expected renamed db")
	}
	if fileExistsForTest(old) {
		t.Fatal("legacy db should have been renamed")
	}
	if !fileExistsForTest(want + "-wal") {
		t.Fatal("expected renamed wal")
	}
}

func fileExistsForTest(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
