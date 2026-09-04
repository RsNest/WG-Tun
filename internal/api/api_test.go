package api_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"transitforge/internal/api"
	"transitforge/internal/auth"
	"transitforge/internal/cli"
	"transitforge/internal/client"
	"transitforge/internal/config"
	"transitforge/internal/logging"
	"transitforge/internal/model"
	"transitforge/internal/reconcile"
	"transitforge/internal/store"
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
		Endpoint: "198.51.100.20:51820", AllowedIPs: []string{"10.200.1.2/32"}, PrivateKeyPath: "/etc/transitforge/keys/wg-a.key",
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
	patched, err := c.PatchMapping(ctx, string(m.ID), model.MappingPatch{Enabled: boolPtr(false)})
	if err != nil || patched.Enabled {
		t.Fatalf("patch disable: %+v %v", patched, err)
	}
	events, err := c.ListEvents(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected audit events")
	}
}

func boolPtr(v bool) *bool { return &v }

func countAction(events []model.AuditEvent, action string) int {
	n := 0
	for _, e := range events {
		if e.Action == action {
			n++
		}
	}
	return n
}

func TestPlanIsReadOnlyAndDoesNotAuditApply(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := c.ListEvents(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	dry := countAction(before, "apply-dry-run")
	apply := countAction(before, "apply")
	pv, err := c.Plan(ctx, string(n.ID))
	if err != nil {
		t.Fatal(err)
	}
	if pv == nil || pv.Plan == "" {
		t.Fatal("plan must return text")
	}
	after, err := c.ListEvents(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if countAction(after, "apply-dry-run") != dry || countAction(after, "apply") != apply {
		t.Fatalf("GET /plan must not create apply audit events: before=%d/%d after=%d/%d", dry, apply, countAction(after, "apply-dry-run"), countAction(after, "apply"))
	}
	if _, err := c.Apply(ctx, string(n.ID), true); err != nil {
		t.Fatal(err)
	}
	audited, err := c.ListEvents(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if countAction(audited, "apply-dry-run") != dry+1 {
		t.Fatalf("POST /apply dry_run must audit apply-dry-run, got %d want %d", countAction(audited, "apply-dry-run"), dry+1)
	}
}

func TestGetActualStateIncludesStatusAndTransport(t *testing.T) {
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
	empty, err := c.GetActualState(ctx, string(n.ID))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Actual != nil && empty.Status != nil && !empty.Status.LastHeartbeat.IsZero() {
		t.Fatal("expected empty actual-state before agent report")
	}
	st := model.ActualState{
		NodeID: n.ID,
		TransportStates: []model.TransportState{{
			NodeID: n.ID, BackendID: b.ID, State: model.TransportWGPrimary,
		}},
	}
	if err := c.PutActualState(ctx, string(n.ID), st); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetActualState(ctx, string(n.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Actual == nil || got.Status == nil {
		t.Fatalf("expected stored actual+status: %+v", got)
	}
	if len(got.Actual.TransportStates) != 1 || got.Actual.TransportStates[0].State != model.TransportWGPrimary {
		t.Fatalf("transport: %+v", got.Actual.TransportStates)
	}
	if got.Status.LastHeartbeat.IsZero() {
		t.Fatal("status last heartbeat missing")
	}
}

func TestPatchBackendMappingAndSni(t *testing.T) {
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
	b.Address = "10.200.1.9"
	gotB, err := c.PatchBackend(ctx, *b)
	if err != nil || gotB.Address != "10.200.1.9" {
		t.Fatalf("patch backend: %+v %v", gotB, err)
	}
	m, err := c.CreateMapping(ctx, model.PortMapping{NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443})
	if err != nil {
		t.Fatal(err)
	}
	port := 8443
	gotM, err := c.PatchMapping(ctx, string(m.ID), model.MappingPatch{PublicPort: &port, Enabled: boolPtr(true)})
	if err != nil || gotM.PublicPort != 8443 || !gotM.Enabled {
		t.Fatalf("patch mapping: %+v %v", gotM, err)
	}
	route, err := c.CreateSniRoute(ctx, model.SniRoute{
		NodeID: n.ID, Listen: ":443",
		Matches: []model.SniMatch{{Default: true, BackendID: b.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	route.Listen = ":8443"
	gotR, err := c.PatchSniRoute(ctx, *route)
	if err != nil || gotR.Listen != ":8443" {
		t.Fatalf("patch sni: %+v %v", gotR, err)
	}
}

func TestRemovedPUTAndAuditRoutes(t *testing.T) {
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
	route, err := c.CreateSniRoute(ctx, model.SniRoute{
		NodeID: n.ID, Listen: ":443",
		Matches: []model.SniMatch{{Default: true, BackendID: b.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(ctx, "PUT", "/api/v1/backends/"+string(b.ID), *b, nil); err == nil {
		t.Fatal("PUT /backends/{id} must not be registered")
	}
	if err := c.Do(ctx, "PUT", "/api/v1/mappings/"+string(m.ID), *m, nil); err == nil {
		t.Fatal("PUT /mappings/{id} must not be registered")
	}
	if err := c.Do(ctx, "PUT", "/api/v1/sni-routes/"+string(route.ID), *route, nil); err == nil {
		t.Fatal("PUT /sni-routes/{id} must not be registered")
	}
	if err := c.Do(ctx, "GET", "/api/v1/audit", nil, nil); err == nil {
		t.Fatal("GET /audit must not be registered")
	}
	if err := c.Do(ctx, "GET", "/api/v1/mappings/"+string(m.ID), nil, nil); err == nil {
		t.Fatal("GET /mappings/{id} must not be registered")
	}
}

func TestMappingEnabledDesiredStateAndPlan(t *testing.T) {
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
	t.Run("enabled appears in desired-state", func(t *testing.T) {
		ds, err := c.DesiredState(ctx, string(n.ID))
		if err != nil {
			t.Fatal(err)
		}
		if len(ds.Mappings) != 1 || ds.Mappings[0].ID != m.ID || !ds.Mappings[0].Enabled {
			t.Fatalf("enabled mapping missing from desired-state: %+v", ds.Mappings)
		}
	})
	comment := reconcile.MappingComment(m.ID)
	spec := "tcp dport 443 -> 10.200.1.2:443"
	if err := c.PutActualState(ctx, string(n.ID), model.ActualState{
		NodeID: n.ID,
		FirewallRules: []model.FirewallRule{{
			Chain: "TRANSITFORGE_DNAT", Comment: comment, Spec: spec, Managed: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled omitted from desired-state", func(t *testing.T) {
		got, err := c.PatchMapping(ctx, string(m.ID), model.MappingPatch{Enabled: boolPtr(false)})
		if err != nil || got.Enabled {
			t.Fatalf("disable: %+v %v", got, err)
		}
		ds, err := c.DesiredState(ctx, string(n.ID))
		if err != nil {
			t.Fatal(err)
		}
		if len(ds.Mappings) != 0 {
			t.Fatalf("disabled mapping must be omitted: %+v", ds.Mappings)
		}
		listed, err := c.ListMappings(ctx)
		if err != nil || len(listed) != 1 || listed[0].Enabled {
			t.Fatalf("catalog must still list disabled mapping: %+v %v", listed, err)
		}
	})
	t.Run("disabling applied mapping yields DELETE plan", func(t *testing.T) {
		pv, err := c.Plan(ctx, string(n.ID))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pv.Plan, "DELETE:") || !strings.Contains(pv.Plan, "firewall") {
			t.Fatalf("expected DELETE firewall plan, got:\n%s", pv.Plan)
		}
	})
	t.Run("converged disabled state is NO CHANGES", func(t *testing.T) {
		if err := c.PutActualState(ctx, string(n.ID), model.ActualState{NodeID: n.ID}); err != nil {
			t.Fatal(err)
		}
		pv, err := c.Plan(ctx, string(n.ID))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pv.Plan, "NO CHANGES") {
			t.Fatalf("expected NO CHANGES after convergence, got:\n%s", pv.Plan)
		}
		res, err := c.Apply(ctx, string(n.ID), true)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Plan, "NO CHANGES") {
			t.Fatalf("apply dry-run should stay idempotent:\n%s", res.Plan)
		}
	})
}

func TestEventsFiltersSinceUntilAction(t *testing.T) {
	c, _, _ := setup(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"})
	if err != nil {
		t.Fatal(err)
	}
	creates, err := c.ListEvents(ctx, "action=create")
	if err != nil {
		t.Fatal(err)
	}
	if countAction(creates, "create") == 0 {
		t.Fatal("action=create should return create events")
	}
	if countAction(creates, "apply-dry-run") != 0 {
		t.Fatal("action=create must not include other actions")
	}
	future, err := c.ListEvents(ctx, "since=2099-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(future) != 0 {
		t.Fatalf("since in the future should be empty: %+v", future)
	}
	aliased, err := c.ListEvents(ctx, "from=2099-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliased) == 0 {
		t.Fatal("from/to must not be aliases; from= should be ignored")
	}
	byNode, err := c.ListEvents(ctx, "node="+string(n.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(byNode) == 0 {
		t.Fatal("node filter should keep node events")
	}
}

func TestReadonlyRejectsWritesButCanReadPlan(t *testing.T) {
	c, url, _ := setup(t)
	ctx := context.Background()
	n, err := c.CreateNode(ctx, model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBackend(ctx, model.Backend{Name: "backend-a", NodeID: n.ID, Address: "10.200.1.2"})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: "reader", Role: model.RoleReadonly})
	if err != nil {
		t.Fatal(err)
	}
	ro := client.New(url, tok.Token, true)
	if _, err := ro.Plan(ctx, string(n.ID)); err != nil {
		t.Fatalf("readonly may GET /plan: %v", err)
	}
	if _, err := ro.GetActualState(ctx, string(n.ID)); err != nil {
		t.Fatalf("readonly may GET actual-state: %v", err)
	}
	if _, err := ro.ListEvents(ctx, ""); err != nil {
		t.Fatalf("readonly may GET /events: %v", err)
	}
	if _, err := ro.PatchBackend(ctx, *b); err == nil {
		t.Fatal("readonly must not PATCH backend")
	}
	if _, err := ro.Apply(ctx, string(n.ID), true); err == nil {
		t.Fatal("readonly must not POST /apply")
	}
	agentTok, err := c.CreateToken(ctx, model.TokenCreateRequest{Name: "ru-edge-1-agent", Role: model.RoleAgent})
	if err != nil {
		t.Fatal(err)
	}
	ag := client.New(url, agentTok.Token, true)
	if _, err := ag.Plan(ctx, string(n.ID)); err != nil {
		t.Fatalf("agent may GET /plan: %v", err)
	}
}

func TestUsersAPIOperatorForbidden(t *testing.T) {
	c, _, _ := setup(t)
	if _, err := c.ListUsers(context.Background()); err == nil {
		t.Fatal("operator token must not list human users")
	}
}
