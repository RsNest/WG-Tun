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
	me               model.PrincipalView
	whoamiErr        error
	listErr          error
	planErr          error
	applyErr         error
	patchErr         error
	createBackendErr error
	nodes            []model.Node
	backends         []model.Backend
	tunnels          []model.Tunnel
	mappings         []model.PortMapping
	sni              []model.SniRoute
	audit            []model.AuditEvent
	actual           map[string]*model.NodeActualState
	plan             string
	applyCalls       int
	dryRunCalls      int
	planCalls        int
	failbackCalls    int
	patchCalls       int
	lastEventsQuery  string
}

func (f *fakeAPI) Whoami(context.Context) (*model.PrincipalView, error) {
	if f.whoamiErr != nil {
		return nil, f.whoamiErr
	}
	cp := f.me
	return &cp, nil
}
func (f *fakeAPI) ListNodes(context.Context) ([]model.Node, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.nodes, nil
}
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
	if f.createBackendErr != nil {
		return nil, f.createBackendErr
	}
	b.ID = "b-new"
	return &b, nil
}
func (f *fakeAPI) PatchBackend(_ context.Context, b model.Backend) (*model.Backend, error) {
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
func (f *fakeAPI) PatchMapping(_ context.Context, id string, patch model.MappingPatch) (*model.PortMapping, error) {
	f.patchCalls++
	if f.patchErr != nil {
		return nil, f.patchErr
	}
	for i := range f.mappings {
		if string(f.mappings[i].ID) == id {
			if patch.Enabled != nil {
				f.mappings[i].Enabled = *patch.Enabled
			}
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
func (f *fakeAPI) PatchSniRoute(_ context.Context, r model.SniRoute) (*model.SniRoute, error) {
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
func (f *fakeAPI) Plan(_ context.Context, _ string) (*model.PlanView, error) {
	f.planCalls++
	if f.planErr != nil {
		return nil, f.planErr
	}
	return &model.PlanView{Plan: f.plan}, nil
}
func (f *fakeAPI) Apply(_ context.Context, _ string, dryRun bool) (*model.ApplyResult, error) {
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	if dryRun {
		f.dryRunCalls++
		return &model.ApplyResult{DryRun: true, Plan: f.plan}, nil
	}
	f.applyCalls++
	return &model.ApplyResult{DryRun: false, Applied: true, Plan: f.plan}, nil
}
func (f *fakeAPI) Failback(context.Context, string, string) error {
	f.failbackCalls++
	return nil
}
func (f *fakeAPI) ListEvents(_ context.Context, query string) ([]model.AuditEvent, error) {
	f.lastEventsQuery = query
	return f.audit, nil
}

func sampleFake() *fakeAPI {
	now := time.Date(2026, 9, 4, 11, 59, 50, 0, time.UTC)
	n := model.Node{ID: "n1", Name: "ru-edge-1", PublicIP: "203.0.113.10", Labels: map[string]string{"mgmt": "10.0.0.8"}}
	b := model.Backend{ID: "b1", Name: "backend-a", NodeID: "n1", Address: "10.200.1.2"}
	return &fakeAPI{
		me:       model.PrincipalView{Name: "operator", Role: model.RoleOperator},
		nodes:    []model.Node{n},
		backends: []model.Backend{b},
		tunnels: []model.Tunnel{{
			ID: "t1", NodeID: "n1", BackendID: "b1", Type: model.TunnelWireGuard,
			InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
			PrivateKeyPath: "/etc/proxyctl/keys/wg-a.key", Endpoint: "198.51.100.20:51820",
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

func doHX(t *testing.T, s *Server, method, path string, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("HX-Request", "true")
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
		if !strings.Contains(html, "proxyctl") {
			t.Fatalf("%s missing product name", path)
		}
	}
	dash := do(t, s, http.MethodGet, "/", cookie, nil).Body.String()
	for _, want := range []string{"ru-edge-1", "203.0.113.10", "WG_PRIMARY", "healthy", `hx-trigger="every 8s"`} {
		if !strings.Contains(dash, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	detail := do(t, s, http.MethodGet, "/nodes/n1", cookie, nil).Body.String()
	for _, want := range []string{"ADD:", "Refresh plan", "Run audited dry-run", `class="plan-line plan-add"`, "Overview", "10.0.0.8"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("node detail missing %q", want)
		}
	}
	if !strings.Contains(detail, "Live apply is not enabled on this controller.") {
		t.Fatal("operator UI must explain that live apply is disabled")
	}
	if strings.Contains(detail, `hx-post="/nodes/n1/apply"`) {
		t.Fatal("Apply must not be enabled while LiveApply is false")
	}
	if strings.Contains(detail, "fail-forward") || strings.Contains(detail, "fail_forward") {
		t.Fatal("user-facing failback must not say fail-forward")
	}
	if fake.planCalls == 0 {
		t.Fatal("node detail must load the plan through the API")
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
	body := pages.Body.String()
	if strings.Contains(body, `hx-post="/nodes/n1/apply"`) {
		t.Fatal("readonly must not see apply controls")
	}
	if !strings.Contains(body, "Refresh plan") {
		t.Fatal("readonly must still see Refresh plan")
	}
	if strings.Contains(body, "Run audited dry-run") || strings.Contains(body, `hx-post="/nodes/n1/dry-run"`) {
		t.Fatal("readonly must not see audited dry-run")
	}
	if strings.Contains(body, "badge role-readonly") == false && !strings.Contains(body, "readonly") {
		t.Fatal("readonly role badge missing")
	}
	beforeApply := fake.applyCalls
	beforeDry := fake.dryRunCalls
	beforePlan := fake.planCalls
	rec := do(t, s, http.MethodGet, "/nodes/n1/plan", cookie, nil)
	if rec.Code != 200 {
		t.Fatalf("readonly plan %d %s", rec.Code, rec.Body.String())
	}
	if fake.planCalls == beforePlan {
		t.Fatal("readonly plan preview must call GET /plan")
	}
	if fake.applyCalls != beforeApply || fake.dryRunCalls != beforeDry {
		t.Fatal("plan preview must not call apply")
	}
	rec = do(t, s, http.MethodPost, "/nodes/n1/apply", cookie, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d %s", rec.Code, rec.Body.String())
	}
	if fake.applyCalls != beforeApply {
		t.Fatal("readonly apply must not call API")
	}
	rec = do(t, s, http.MethodPost, "/nodes/n1/dry-run", cookie, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("readonly dry-run expected 403, got %d %s", rec.Code, rec.Body.String())
	}
	if fake.dryRunCalls != beforeDry {
		t.Fatal("readonly dry-run must not call API")
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
	op := do(t, s, http.MethodPost, "/login", nil, url.Values{"token": {"good-op"}})
	cs := op.Result().Cookies()
	if len(cs) == 0 {
		t.Fatal("login cookie missing")
	}
	out := do(t, s, http.MethodPost, "/logout", cs[0], nil)
	if out.Code != http.StatusSeeOther {
		t.Fatalf("logout %d", out.Code)
	}
	rec = do(t, s, http.MethodGet, "/", cs[0], nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logged-out session should redirect, got %d", rec.Code)
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
	rec = do(t, s, http.MethodGet, "/static/app.js", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "dash-refresh-error") {
		t.Fatalf("app.js %d", rec.Code)
	}
}

func TestLiveApplyDisabledRejectsApply(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "sekrit-token-value")
	before := fake.applyCalls
	rec := do(t, s, http.MethodPost, "/nodes/n1/apply", cookie, nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Live apply is not enabled on this controller.") {
		t.Fatalf("missing explanation: %s", rec.Body.String())
	}
	if fake.applyCalls != before {
		t.Fatal("disabled live apply must not call POST /apply")
	}
	rec = do(t, s, http.MethodGet, "/nodes/n1/plan", cookie, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ADD:") {
		t.Fatalf("plan preview %d %s", rec.Code, rec.Body.String())
	}
	rec = doHX(t, s, http.MethodPost, "/nodes/n1/dry-run", cookie, nil)
	if rec.Code != 200 {
		t.Fatalf("audited dry-run %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Audited dry-run recorded.") || !strings.Contains(body, "ADD:") {
		t.Fatalf("audited dry-run missing result: %s", body)
	}
	if strings.Contains(body, "<html") {
		t.Fatal("audited dry-run must return a plan fragment")
	}
	if fake.dryRunCalls != 1 {
		t.Fatalf("audited dry-run must call Apply(dry_run=true), got %d", fake.dryRunCalls)
	}
	if fake.applyCalls != before {
		t.Fatal("audited dry-run must not count as live apply")
	}
}

func TestEventsFilterQueryParams(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "sekrit-token-value")
	rec := do(t, s, http.MethodGet, "/events?node=ru-edge-1&since=2026-09-01&until=2026-09-04&action=create", cookie, nil)
	if rec.Code != 200 {
		t.Fatalf("events %d %s", rec.Code, rec.Body.String())
	}
	html := rec.Body.String()
	if strings.Contains(html, `name="from"`) || strings.Contains(html, `name="to"`) {
		t.Fatal("events UI must use since/until, not from/to")
	}
	if !strings.Contains(html, `name="since"`) || !strings.Contains(html, `name="until"`) || !strings.Contains(html, `name="action"`) {
		t.Fatal("events UI missing canonical filters")
	}
	if !strings.Contains(html, "Apply filters") || !strings.Contains(html, "Clear filters") {
		t.Fatal("events UI missing apply/clear filters")
	}
	if !strings.Contains(fake.lastEventsQuery, "node=ru-edge-1") || !strings.Contains(fake.lastEventsQuery, "action=create") {
		t.Fatalf("events filters must be forwarded to the API, got %q", fake.lastEventsQuery)
	}
}

func TestPlanNoChangesAndSemanticClasses(t *testing.T) {
	fake := sampleFake()
	fake.plan = ""
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := do(t, s, http.MethodGet, "/nodes/n1", cookie, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "NO CHANGES") {
		t.Fatalf("empty plan should render NO CHANGES: %d %s", rec.Code, rec.Body.String())
	}
	fake.plan = "CHANGE: mapping TCP/443\nDELETE: mapping UDP/53\nCONFLICT: backend-a\nNO CHANGES"
	rec = doHX(t, s, http.MethodGet, "/nodes/n1/plan", cookie, nil)
	body := rec.Body.String()
	for _, want := range []string{`plan-line plan-change`, `plan-line plan-delete`, `plan-line plan-conflict`, `plan-line plan-none`} {
		if !strings.Contains(body, want) {
			t.Fatalf("plan fragment missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "<nav") || strings.Contains(body, "<html") {
		t.Fatal("plan fragment must not include the page shell")
	}
}

func TestDashboardPollFailureKeepsFragmentContract(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	ok := doHX(t, s, http.MethodGet, "/", cookie, nil)
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), `id="node-cards"`) {
		t.Fatalf("dashboard cards %d %s", ok.Code, ok.Body.String())
	}
	if strings.Contains(ok.Body.String(), "<nav") {
		t.Fatal("dashboard poll must return cards only")
	}
	fake.listErr = fmt.Errorf("connection refused")
	fail := doHX(t, s, http.MethodGet, "/", cookie, nil)
	if fail.Code != http.StatusBadGateway {
		t.Fatalf("poll failure expected 502, got %d %s", fail.Code, fail.Body.String())
	}
	if strings.Contains(fail.Body.String(), "<html") || strings.Contains(fail.Body.String(), "goroutine") {
		t.Fatalf("poll failure must not replace the page: %s", fail.Body.String())
	}
}

func TestAPIUnavailablePage(t *testing.T) {
	fake := sampleFake()
	fake.listErr = fmt.Errorf("connection refused")
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := do(t, s, http.MethodGet, "/", cookie, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "API unavailable") && !strings.Contains(body, "did not respond") {
		t.Fatalf("missing operator error: %s", body)
	}
	if strings.Contains(body, "goroutine") || strings.Contains(body, "runtime/debug") {
		t.Fatal("must not expose stack traces")
	}
}

func TestUnauthorizedAPIRedirectsToLogin(t *testing.T) {
	fake := sampleFake()
	fake.listErr = model.Unauthorized("token expired")
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := do(t, s, http.MethodGet, "/nodes", cookie, nil)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/login") {
		t.Fatalf("expired API token should redirect to login: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	hxrec := doHX(t, s, http.MethodGet, "/nodes", cookie, nil)
	if hxrec.Code != http.StatusUnauthorized {
		t.Fatalf("HX unauthorized expected 401, got %d", hxrec.Code)
	}
	if hxrec.Header().Get("HX-Redirect") != "/login" {
		t.Fatalf("HX-Redirect=%q", hxrec.Header().Get("HX-Redirect"))
	}
}

func TestExpiredSessionHXRedirect(t *testing.T) {
	s := testUI(t, sampleFake())
	rec := doHX(t, s, http.MethodGet, "/", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("HX-Redirect") != "/login" {
		t.Fatalf("HX-Redirect=%q", rec.Header().Get("HX-Redirect"))
	}
}

func TestEmptyStates(t *testing.T) {
	fake := sampleFake()
	fake.nodes = nil
	fake.backends = nil
	fake.tunnels = nil
	fake.mappings = nil
	fake.sni = nil
	fake.audit = nil
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	cases := map[string]string{
		"/":           "No nodes configured yet.",
		"/backends":   "No backends configured yet.",
		"/tunnels":    "No tunnels configured yet.",
		"/mappings":   "No mappings configured yet.",
		"/sni-routes": "No SNI routes configured yet.",
		"/events":     "No events recorded yet.",
	}
	for path, want := range cases {
		rec := do(t, s, http.MethodGet, path, cookie, nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s missing %q (%d %s)", path, want, rec.Code, rec.Body.String())
		}
	}
}

func TestUnreportedAgentShowsUnknown(t *testing.T) {
	fake := sampleFake()
	fake.actual = map[string]*model.NodeActualState{}
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	dash := do(t, s, http.MethodGet, "/", cookie, nil).Body.String()
	if !strings.Contains(dash, "unknown") {
		t.Fatal("unreported agent should show unknown status")
	}
	if !strings.Contains(dash, "Agent has not reported actual state yet.") {
		t.Fatal("missing unreported-agent empty state")
	}
	if strings.Contains(dash, "unhealthy") {
		t.Fatal("missing agent report must not look unhealthy")
	}
}

func TestMappingToggleHTMX(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := doHX(t, s, http.MethodPatch, "/mappings/m1", cookie, url.Values{"enabled": {"false"}})
	if rec.Code != 200 {
		t.Fatalf("toggle %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<nav") {
		t.Fatal("mapping toggle must return a table fragment")
	}
	if !strings.Contains(body, `id="mappings-table"`) {
		t.Fatal("mapping toggle missing table fragment")
	}
	if !strings.Contains(body, "disabled") {
		t.Fatalf("expected disabled status, got %s", body)
	}
	if fake.patchCalls != 1 || fake.mappings[0].Enabled {
		t.Fatal("PATCH must persist enabled=false")
	}
	fake.patchErr = model.ErrConflict("mapping in use")
	fail := doHX(t, s, http.MethodPatch, "/mappings/m1", cookie, url.Values{"enabled": {"true"}})
	if fail.Code != http.StatusConflict {
		t.Fatalf("failed toggle expected 409, got %d %s", fail.Code, fail.Body.String())
	}
	if fake.mappings[0].Enabled {
		t.Fatal("failed PATCH must leave previous enabled state")
	}
}

func TestFormValidationPreservesValues(t *testing.T) {
	fake := sampleFake()
	fake.createBackendErr = model.Validation("address must be an overlay IPv4")
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := do(t, s, http.MethodPost, "/backends", cookie, url.Values{
		"name":    {"kept-name"},
		"node_id": {"n1"},
		"address": {"203.0.113.9"},
		"token":   {"should-not-echo"},
	})
	if rec.Code != 200 {
		t.Fatalf("validation should re-render, got %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "kept-name") {
		t.Fatal("submitted name must be preserved")
	}
	if !strings.Contains(body, "address must be an overlay IPv4") {
		t.Fatal("validation message missing")
	}
	if strings.Contains(body, "should-not-echo") {
		t.Fatal("secret form fields must not be repopulated")
	}
}

func TestFailbackShownOnlyWhenSSHPrimary(t *testing.T) {
	fake := sampleFake()
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	body := do(t, s, http.MethodGet, "/nodes/n1", cookie, nil).Body.String()
	if strings.Contains(body, "id=\"failback-h\"") || strings.Contains(body, ">Failback<") {
		t.Fatal("Failback must be hidden while transport is WG_PRIMARY")
	}
	fake.actual["n1"].Actual.TransportStates = []model.TransportState{{BackendID: "b1", State: model.TransportSSHPrimary}}
	body = do(t, s, http.MethodGet, "/nodes/n1", cookie, nil).Body.String()
	if !strings.Contains(body, "Switch this node from SSH fallback back to WireGuard?") {
		t.Fatal("Failback confirm text missing")
	}
	if strings.Contains(body, "fail_forward") {
		t.Fatal("Failback UI must not use fail_forward")
	}
}

func TestConflictDryRunRendersWarning(t *testing.T) {
	fake := sampleFake()
	fake.applyErr = model.ErrConflict("CONFLICT: mapping TCP/443 already claimed")
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	rec := doHX(t, s, http.MethodPost, "/nodes/n1/dry-run", cookie, nil)
	if rec.Code != 200 {
		t.Fatalf("conflict dry-run %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "conflict") {
		t.Fatalf("expected conflict styling: %s", body)
	}
	if !strings.Contains(body, "CONFLICT: mapping TCP/443 already claimed") {
		t.Fatalf("controller conflict text missing: %s", body)
	}
	if strings.Contains(body, "goroutine") {
		t.Fatal("must not dump stacks")
	}
}

func TestEventSecretsRedacted(t *testing.T) {
	fake := sampleFake()
	fake.audit = []model.AuditEvent{{
		Timestamp: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
		Actor:     "operator", Action: "token.add", Resource: "token", ResourceID: "t1",
		Detail: "Bearer tok_live_secret", Success: false,
	}}
	s := testUI(t, fake)
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	body := do(t, s, http.MethodGet, "/events", cookie, nil).Body.String()
	if strings.Contains(body, "tok_live_secret") || strings.Contains(body, "Bearer") {
		t.Fatal("event detail leaked a secret")
	}
}

func TestTunnelFormUsesKeyPathOnly(t *testing.T) {
	s := testUI(t, sampleFake())
	cookie := sessionCookie(t, s, "operator", model.RoleOperator, "tok")
	body := do(t, s, http.MethodGet, "/tunnels", cookie, nil).Body.String()
	if !strings.Contains(body, `name="private_key_path"`) {
		t.Fatal("tunnel form must accept a key path")
	}
	if strings.Contains(body, `name="private_key"`) {
		t.Fatal("tunnel form must not accept raw private key material")
	}
}
