package haproxy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxyctl/internal/haproxy"
	"proxyctl/internal/model"
	"proxyctl/internal/reconcile"
	"proxyctl/internal/testhost"
)

func TestRenderPreservesUnmanagedAndRestoresOnValidateFail(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	unmanaged := "global\n    daemon\nfrontend other\n    bind :80\n    mode http\n"
	if err := os.WriteFile(cfg, []byte(unmanaged), 0o600); err != nil {
		t.Fatal(err)
	}
	h := testhost.New()
	m := &haproxy.Manager{Runner: h, ConfigPath: cfg, BackupDir: filepath.Join(dir, "bak")}
	routes := []model.SniRoute{{
		ID: "r1", NodeID: "n", Listen: ":443",
		Matches: []model.SniMatch{
			{Match: "example.com", Backend: "backend-a", BackendID: "be1"},
			{Default: true, Backend: "backend-b", BackendID: "be2"},
		},
	}}
	backends := []model.Backend{
		{ID: "be1", Name: "backend-a", Address: "10.200.1.2"},
		{ID: "be2", Name: "backend-b", Address: "10.200.2.2"},
	}
	if err := m.Apply(context.Background(), routes, backends); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cfg)
	if !strings.Contains(string(body), "bind :80") {
		t.Fatal("unmanaged section lost")
	}
	if !strings.Contains(string(body), haproxy.BeginMarker) || !strings.Contains(string(body), "example.com") {
		t.Fatalf("managed missing: %s", body)
	}
	digest, _, _, err := m.Discover(context.Background(), routes)
	if err != nil {
		t.Fatal(err)
	}
	if digest != reconcile.SniDigest(routes) {
		t.Fatalf("digest %q vs %q", digest, reconcile.SniDigest(routes))
	}

	h.FailOn = "haproxy -c"
	orig := string(body)
	err = m.Apply(context.Background(), routes, backends)
	if err == nil {
		t.Fatal("expected validation conflict")
	}
	if !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatalf("%v", err)
	}
	after, _ := os.ReadFile(cfg)
	if string(after) != orig {
		t.Fatal("config should be restored after validation failure")
	}
	for _, c := range h.Calls {
		if len(c) > 1 && c[0] == "systemctl" && c[1] == "restart" {
			t.Fatal("must not restart haproxy")
		}
	}
}

func TestReloadFailureRestores(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	h := testhost.New()
	m := &haproxy.Manager{Runner: h, ConfigPath: cfg, BackupDir: filepath.Join(dir, "bak")}
	routes := []model.SniRoute{{
		ID: "r1", NodeID: "n", Listen: ":443",
		Matches: []model.SniMatch{{Default: true, Backend: "backend-a", BackendID: "be1"}},
	}}
	backends := []model.Backend{{ID: "be1", Name: "backend-a", Address: "10.200.1.2"}}
	if err := m.Apply(context.Background(), routes, backends); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(cfg)
	h.FailOn = "systemctl reload"
	if err := m.Apply(context.Background(), routes, backends); err == nil {
		t.Fatal("expected reload conflict")
	}
	after, _ := os.ReadFile(cfg)
	if string(after) != string(good) {
		t.Fatal("should restore on reload failure")
	}
}

func TestExternalReloadSkipsSystemctl(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	h := testhost.New()
	m := &haproxy.Manager{Runner: h, ConfigPath: cfg, BackupDir: filepath.Join(dir, "bak"), ReloadMode: "external"}
	routes := []model.SniRoute{{
		ID: "r1", NodeID: "n", Listen: ":443",
		Matches: []model.SniMatch{{Default: true, Backend: "backend-a", BackendID: "be1"}},
	}}
	backends := []model.Backend{{ID: "be1", Name: "backend-a", Address: "10.200.1.2"}}
	if err := m.Apply(context.Background(), routes, backends); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cfg)
	if !strings.Contains(string(body), haproxy.BeginMarker) {
		t.Fatal("expected managed section written")
	}
	for _, c := range h.Calls {
		if len(c) > 0 && c[0] == "systemctl" {
			t.Fatalf("external reload must not call systemctl: %v", c)
		}
	}
	if err := m.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Calls {
		if len(c) > 0 && c[0] == "systemctl" {
			t.Fatalf("external rollback must not call systemctl: %v", c)
		}
	}
}

func TestBindCollision(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(cfg, []byte("frontend x\n    bind :443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &haproxy.Manager{Runner: testhost.New(), ConfigPath: cfg, BackupDir: filepath.Join(dir, "bak")}
	routes := []model.SniRoute{{
		ID: "r1", NodeID: "n", Listen: ":443",
		Matches: []model.SniMatch{{Default: true, Backend: "backend-a", BackendID: "be1"}},
	}}
	err := m.Apply(context.Background(), routes, []model.Backend{{ID: "be1", Name: "backend-a", Address: "10.200.1.2"}})
	if err == nil {
		t.Fatal("expected collision")
	}
}
