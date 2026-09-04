package api_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"proxyctl/internal/api"
	"proxyctl/internal/auth"
	"proxyctl/internal/cli"
	"proxyctl/internal/client"
	"proxyctl/internal/config"
	"proxyctl/internal/logging"
	"proxyctl/internal/model"
	"proxyctl/internal/store"
)

func setup(t *testing.T) (*client.Client, string, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenSQLite(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := auth.New(st, true, 5*time.Minute)
	tokFile := filepath.Join(dir, "bootstrap.token")
	if _, err := a.EnsureBootstrapToken(context.Background(), tokFile); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tokFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultController()
	cfg.TLS.Required = false
	cfg.DataDir = dir
	srv := api.New(cfg, st, a, logging.New("test"), api.Capabilities{})
	srv.SetReady(true)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	tok := strings.TrimSpace(string(b))
	return client.New(hs.URL, tok, true), hs.URL, tokFile
}

func TestHealthAndReady(t *testing.T) {
	cli, url, _ := setup(t)
	_ = url
	if err := cli.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestE2ENodeBackendMappingDryRun(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBackend(ctx, model.Backend{Name: "backend-a", NodeID: n.ID, Address: "10.200.1.2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateMapping(ctx, model.PortMapping{NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoUDP, PublicPort: 51821, BackendPort: 51820}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateMapping(ctx, model.PortMapping{NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateTunnel(ctx, model.Tunnel{
		NodeID: n.ID, BackendID: b.ID, Type: model.TunnelWireGuard, InterfaceName: "wg-a",
		LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2", ListenPort: 51820,
		Endpoint: "198.51.100.20:51820", AllowedIPs: []string{"10.200.1.2/32"}, PrivateKeyPath: "/etc/proxyctl/keys/wg-a.key",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(ctx, string(n.ID), true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Applied {
		t.Fatalf("dry-run must not apply: %+v", res)
	}
	if !strings.Contains(res.Plan, "ADD:") {
		t.Fatalf("expected ADD plan, got:\n%s", res.Plan)
	}
}

func TestCLIAgainstAPI(t *testing.T) {
	_, url, tokFile := setup(t)
	var out strings.Builder
	opt := cli.Options{Controller: url, TokenFile: tokFile, Insecure: true, Stdout: &out, Stderr: &out}
	if err := cli.Run([]string{"node", "add", "--name", "ru-edge-1", "--public-ip", "203.0.113.10"}, opt); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cli.Run([]string{"backend", "add", "--name", "backend-a", "--node", "ru-edge-1", "--address", "10.200.1.2"}, opt); err != nil {
		t.Fatal(err)
	}
	if err := cli.Run([]string{"mapping", "add", "--node", "ru-edge-1", "--backend", "backend-a", "--protocol", "UDP", "--public-port", "51821", "--backend-port", "51820"}, opt); err != nil {
		t.Fatal(err)
	}
	if err := cli.Run([]string{"mapping", "add", "--node", "ru-edge-1", "--backend", "backend-a", "--protocol", "TCP", "--public-port", "443", "--backend-port", "443"}, opt); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := cli.Run([]string{"apply", "--node", "ru-edge-1", "--dry-run"}, opt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ADD:") {
		t.Fatalf("cli dry-run plan: %s", out.String())
	}
}

func TestCreateAgentTokenAndWhoami(t *testing.T) {
	c, url, tokFile := setup(t)
	ctx := context.Background()
	me, err := c.Whoami(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if me.Role != model.RoleOperator {
		t.Fatalf("bootstrap role %s", me.Role)
	}
	created, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: "ru-edge-1-agent", Role: model.RoleAgent})
	if err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || created.Role != model.RoleAgent {
		t.Fatalf("%+v", created)
	}
	agent := client.New(url, created.Token, true)
	got, err := agent.Whoami(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != model.RoleAgent || got.Name != "ru-edge-1-agent" {
		t.Fatalf("%+v", got)
	}
	if _, err := agent.CreateToken(ctx, model.TokenCreateRequest{Name: "nope", Role: model.RoleAgent}); err == nil {
		t.Fatal("agent must not mint tokens")
	}
	var out strings.Builder
	opt := cli.Options{Controller: url, TokenFile: tokFile, Insecure: true, Stdout: &out, Stderr: &out}
	if err := cli.Run([]string{"token", "add", "--name", "cli-agent", "--role", "agent"}, opt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"role": "agent"`) {
		t.Fatalf("cli mint: %s", out.String())
	}
	out.Reset()
	secretPath := filepath.Join(t.TempDir(), "agent.token")
	if err := cli.Run([]string{"token", "add", "--name", "cli-agent-file", "--role", "agent", "--out-file", secretPath}, opt); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.TrimSpace(string(b))
	if secret == "" {
		t.Fatal("out-file empty")
	}
	if strings.Contains(out.String(), secret) {
		t.Fatal("--out-file must not print the token on stdout")
	}
	if st, err := os.Stat(secretPath); err != nil {
		t.Fatal(err)
	} else if st.Size() == 0 {
		t.Fatal("out-file empty")
	}
}

func TestUnauthorized(t *testing.T) {
	_, url, _ := setup(t)
	c := client.New(url, "deadbeef", true)
	_, err := c.ListNodes(context.Background())
	if err == nil {
		t.Fatal("expected unauthorized")
	}
}

func TestReadyzBeforeInit(t *testing.T) {
	dir := t.TempDir()
	st, err := store.OpenSQLite(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := auth.New(st, false, time.Minute)
	cfg := config.DefaultController()
	cfg.TLS.Required = false
	srv := api.New(cfg, st, a, logging.New("test"), api.Capabilities{})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	c := client.New(hs.URL, "", true)
	if err := c.Do(context.Background(), "GET", "/readyz", nil, nil); err == nil {
		t.Fatal("readyz should fail before SetReady")
	}
}

func TestGetActualAuditAndMappingPatch(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBackend(ctx, model.Backend{Name: "backend-a", NodeID: n.ID, Address: "10.200.1.2"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := c.CreateMapping(ctx, model.PortMapping{NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Enabled {
		t.Fatal("created mapping should be enabled")
	}
	st, err := c.GetActualState(ctx, string(n.ID))
	if err != nil {
		t.Fatal(err)
	}
	if st.Actual != nil && st.Status != nil && !st.Status.LastHeartbeat.IsZero() {
		t.Fatal("expected empty actual-state before agent report")
	}
	got, err := c.GetBackend(ctx, string(b.ID))
	if err != nil || got.Name != "backend-a" {
		t.Fatalf("%+v %v", got, err)
	}
	routes, err := c.ListSniRoutes(ctx)
	if err != nil || routes == nil {
		t.Fatalf("sni list %v %v", routes, err)
	}
	patched, err := c.PatchMapping(ctx, string(m.ID), false)
	if err != nil || patched.Enabled {
		t.Fatalf("patch disable: %+v %v", patched, err)
	}
	events, err := c.ListAudit(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected audit events")
	}
}
