package webui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"proxyctl/internal/model"
)

type fakeAPI struct {
	me            model.PrincipalView
	whoamiErr     error
	nodes         []model.Node
	backends      []model.Backend
	tunnels       []model.Tunnel
	mappings      []model.PortMapping
	sni           []model.SniRoute
	audit         []model.AuditEvent
	actual        map[string]*model.NodeActualState
	plan          string
	applyCalls    int
	failbackCalls int
	patchCalls    int
}

func (f *fakeAPI) Whoami(context.Context) (*model.PrincipalView, error) {
	if f.whoamiErr != nil {
		return nil, f.whoamiErr
	}
	cp := f.me
	return &cp, nil
}
func (f *fakeAPI) ListNodes(context.Context) ([]model.Node, error) { return f.nodes, nil }
func (f *fakeAPI) GetNode(_ context.Context, id string) (*model.Node, error) {
	for i := range f.nodes {
		if string(f.nodes[i].ID) == id {
			return &f.nodes[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeAPI) ListBackends(context.Context) ([]model.Backend, error) { return f.backends, nil }
func (f *fakeAPI) GetBackend(_ context.Context, id string) (*model.Backend, error) {
	for i := range f.backends {
		if string(f.backends[i].ID) == id {
			return &f.backends[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeAPI) CreateBackend(_ context.Context, b model.Backend) (*model.Backend, error) {
	b.ID = "b-new"
	return &b, nil
}
func (f *fakeAPI) UpdateBackend(_ context.Context, b model.Backend) (*model.Backend, error) {
	return &b, nil
}
func (f *fakeAPI) ListTunnels(context.Context) ([]model.Tunnel, error) { return f.tunnels, nil }
func (f *fakeAPI) CreateTunnel(_ context.Context, t model.Tunnel) (*model.Tunnel, error) {
	t.ID = "t-new"
	return &t, nil
}
func (f *fakeAPI) ListMappings(context.Context) ([]model.PortMapping, error) { return f.mappings, nil }
func (f *fakeAPI) CreateMapping(_ context.Context, m model.PortMapping) (*model.PortMapping, error) {
	m.ID = "m-new"
	m.Enabled = true
	return &m, nil
}
func (f *fakeAPI) UpdateMapping(_ context.Context, m model.PortMapping) (*model.PortMapping, error) {
	return &m, nil
}
func (f *fakeAPI) PatchMapping(_ context.Context, id string, enabled bool) (*model.PortMapping, error) {
	f.patchCalls++
	for i := range f.mappings {
		if string(f.mappings[i].ID) == id {
			f.mappings[i].Enabled = enabled
			return &f.mappings[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeAPI) DeleteMapping(context.Context, string) error             { return nil }
func (f *fakeAPI) ListSniRoutes(context.Context) ([]model.SniRoute, error) { return f.sni, nil }
func (f *fakeAPI) GetSniRoute(_ context.Context, id string) (*model.SniRoute, error) {
	for i := range f.sni {
		if string(f.sni[i].ID) == id {
			return &f.sni[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeAPI) CreateSniRoute(_ context.Context, r model.SniRoute) (*model.SniRoute, error) {
	r.ID = "s-new"
	return &r, nil
}
func (f *fakeAPI) UpdateSniRoute(_ context.Context, r model.SniRoute) (*model.SniRoute, error) {
	return &r, nil
}
func (f *fakeAPI) DesiredState(_ context.Context, nodeID string) (*model.DesiredState, error) {
	n, err := f.GetNode(context.Background(), nodeID)
	if err != nil {
		return nil, err
	}
	return &model.DesiredState{Node: *n, Backends: f.backends, Tunnels: f.tunnels, Mappings: f.mappings, SniRoutes: f.sni}, nil
}
func (f *fakeAPI) GetActualState(_ context.Context, nodeID string) (*model.NodeActualState, error) {
	if f.actual == nil {
		return &model.NodeActualState{}, nil
	}
	if v := f.actual[nodeID]; v != nil {
		return v, nil
	}
	return &model.NodeActualState{}, nil
}
func (f *fakeAPI) Apply(context.Context, string, bool) (*model.ApplyResult, error) {
	f.applyCalls++
	return &model.ApplyResult{DryRun: true, Plan: f.plan}, nil
}
func (f *fakeAPI) Failback(context.Context, string, string) error {
	f.failbackCalls++
	return nil
}
func (f *fakeAPI) ListAudit(context.Context, string) ([]model.AuditEvent, error) {
	return f.audit, nil
}

func sampleFake() *fakeAPI {
	now := time.Date(2026, 9, 4, 11, 59, 50, 0, time.UTC)
	n := model.Node{ID: "n1", Name: "ru-edge-1", PublicIP: "203.0.113.10"}
	b := model.Backend{ID: "b1", Name: "backend-a", NodeID: "n1", Address: "10.200.1.2"}
	return &fakeAPI{
		me:       model.PrincipalView{Name: "operator", Role: model.RoleOperator},
		nodes:    []model.Node{n},
		backends: []model.Backend{b},
		tunnels: []model.Tunnel{{
			ID: "t1", NodeID: "n1", BackendID: "b1", Type: model.TunnelWireGuard,
			InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
			PrivateKeyPath: "/etc/proxyctl/keys/wg-a.key",
		}},
		mappings: []model.PortMapping{{
			ID: "m1", NodeID: "n1", BackendID: "b1", Protocol: model.ProtoTCP,
			PublicPort: 443, BackendPort: 443, Enabled: true,
		}},
		sni: []model.SniRoute{{
			ID: "s1", NodeID: "n1", Listen: ":443",
			Matches: []model.SniMatch{{Match: "app.example.com", Backend: "backend-a"}, {Default: true, Backend: "backend-a"}},
		}},
		audit: []model.AuditEvent{{
			Timestamp: now, Actor: "operator", Action: "create", Resource: "node", ResourceID: "n1", Success: true,
		}},
		plan: "ADD: tunnel wg-a\nADD: mapping TCP/443",
		actual: map[string]*model.NodeActualState{
			"n1": {
				Status: &model.AgentStatus{Healthy: true, LastHeartbeat: now, LastReconcile: now},
				Actual: &model.ActualState{
					TransportStates: []model.TransportState{{BackendID: "b1", State: model.TransportWGPrimary}},
					Tunnels: []model.TunnelActual{{
						TunnelID: "t1", InterfaceName: "wg-a", InterfacePresent: true,
						HandshakeAgeSec: 12, RxBytes: 4096, TxBytes: 2048,
					}},
				},
			},
		},
	}
}

func testUI(t *testing.T, fake *fakeAPI) *Server {
	t.Helper()
	srv, err := New(Config{
		NewClient: func(string) API { return fake },
		Now:       func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func sessionCookie(t *testing.T, s *Server, name string, role model.Role, token string) *http.Cookie {
	t.Helper()
	sess, err := s.sessions.put(token, name, role)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.setSessionCookie(rec, sess)
	cs := rec.Result().Cookies()
	if len(cs) == 0 {
		t.Fatal("no cookie")
	}
	return cs[0]
}

func do(t *testing.T, s *Server, method, path string, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRenderPages(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "sekrit-token-value")
	pages := []string{"/", "/nodes", "/nodes/n1", "/backends", "/backends/b1", "/tunnels", "/mappings", "/sni-routes", "/sni-routes/s1", "/events"}
	for _, path := range pages {
		rec := do(t, s, http.MethodGet, path, cookie, nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		html := rec.Body.String()
		if strings.Contains(html, "sekrit-token-value") {
			t.Fatalf("%s leaked session token", path)
		}
		if strings.Contains(html, "BEGIN PRIVATE KEY") || strings.Contains(html, "private_key\":") {
			t.Fatalf("%s leaked key material", path)
		}
	}
	dash := do(t, s, http.MethodGet, "/", cookie, nil).Body.String()
	for _, want := range []string{"ru-edge-1", "203.0.113.10", "WG_PRIMARY", "healthy"} {
		if !strings.Contains(dash, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	detail := do(t, s, http.MethodGet, "/nodes/n1", cookie, nil).Body.String()
	if !strings.Contains(detail, "ADD:") {
		t.Fatalf("node detail missing plan: %s", detail)
	}
}

func TestReadonlyWriteRejected(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "reader", model.RoleReadonly, "readonly-token")
	pages := do(t, s, http.MethodGet, "/nodes/n1", cookie, nil)
	if pages.Code != 200 {
		t.Fatalf("readonly detail %d %s", pages.Code, pages.Body.String())
	}
	if strings.Contains(pages.Body.String(), `hx-post="/nodes/n1/apply"`) {
		t.Fatal("readonly must not see apply controls")
	}
	before := fake.applyCalls
	rec := do(t, s, http.MethodPost, "/nodes/n1/apply", cookie, url.Values{"dry_run": {"true"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d %s", rec.Code, rec.Body.String())
	}
	if fake.applyCalls != before {
		t.Fatal("readonly apply must not call API")
	}
	rec = do(t, s, http.MethodPatch, "/mappings/m1", cookie, url.Values{"enabled": {"false"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("patch expected 403, got %d", rec.Code)
	}
	if fake.patchCalls != 0 {
		t.Fatal("readonly patch must not call API")
	}
}

func TestLoginAndLogout(t *testing.T) {
	state := sampleFake()
	s, err := New(Config{NewClient: func(token string) API {
		f := *state
		switch token {
		case "good-op":
			f.me = model.PrincipalView{Name: "bootstrap-operator", Role: model.RoleOperator}
		case "good-ro":
			f.me = model.PrincipalView{Name: "reader", Role: model.RoleReadonly}
		case "agent-tok":
			f.me = model.PrincipalView{Name: "edge", Role: model.RoleAgent}
		default:
			f.whoamiErr = fmt.Errorf("unauthorized")
		}
		return &f
	}})
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, http.MethodPost, "/login", nil, url.Values{"token": {"good-op"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "good-op") {
		t.Fatal("token echoed")
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Fatal("missing session cookie")
	}
	rec = do(t, s, http.MethodPost, "/login", nil, url.Values{"token": {"agent-tok"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "login") {
		t.Fatalf("agent should be rejected: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = do(t, s, http.MethodGet, "/", nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated / should redirect, got %d", rec.Code)
	}
}

func TestStaticAssets(t *testing.T) {
	s := testUI(t, sampleFake())
	rec := do(t, s, http.MethodGet, "/static/app.css", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "--healthy") {
		t.Fatalf("css %d", rec.Code)
	}
	rec = do(t, s, http.MethodGet, "/static/htmx.min.js", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "htmx") {
		t.Fatalf("htmx %d", rec.Code)
	}
}
