package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"proxyctl/internal/client"
	"proxyctl/internal/model"
)

// API is the controller REST surface the UI is allowed to call.
// Tests substitute a fake; production uses the in-process client.
type API interface {
	Whoami(ctx context.Context) (*model.PrincipalView, error)
	ListNodes(ctx context.Context) ([]model.Node, error)
	GetNode(ctx context.Context, id string) (*model.Node, error)
	ListBackends(ctx context.Context) ([]model.Backend, error)
	GetBackend(ctx context.Context, id string) (*model.Backend, error)
	CreateBackend(ctx context.Context, b model.Backend) (*model.Backend, error)
	UpdateBackend(ctx context.Context, b model.Backend) (*model.Backend, error)
	ListTunnels(ctx context.Context) ([]model.Tunnel, error)
	CreateTunnel(ctx context.Context, t model.Tunnel) (*model.Tunnel, error)
	ListMappings(ctx context.Context) ([]model.PortMapping, error)
	CreateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error)
	UpdateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error)
	PatchMapping(ctx context.Context, id string, enabled bool) (*model.PortMapping, error)
	DeleteMapping(ctx context.Context, id string) error
	ListSniRoutes(ctx context.Context) ([]model.SniRoute, error)
	GetSniRoute(ctx context.Context, id string) (*model.SniRoute, error)
	CreateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error)
	UpdateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error)
	DesiredState(ctx context.Context, nodeID string) (*model.DesiredState, error)
	GetActualState(ctx context.Context, nodeID string) (*model.NodeActualState, error)
	Apply(ctx context.Context, nodeID string, dryRun bool) (*model.ApplyResult, error)
	Failback(ctx context.Context, nodeID, backend string) error
	ListAudit(ctx context.Context, query string) ([]model.AuditEvent, error)
}

type liveAPI struct {
	c *client.Client
}

func newLiveAPI(token string, rt http.RoundTripper) API {
	c := client.New("http://webui.internal", token, true)
	c.UserAgent = "proxyctl-webui"
	c.HTTP = &http.Client{Transport: rt, Timeout: c.HTTP.Timeout}
	return &liveAPI{c: c}
}

func (a *liveAPI) Whoami(ctx context.Context) (*model.PrincipalView, error) {
	return a.c.Whoami(ctx)
}
func (a *liveAPI) ListNodes(ctx context.Context) ([]model.Node, error) {
	return a.c.ListNodes(ctx)
}
func (a *liveAPI) GetNode(ctx context.Context, id string) (*model.Node, error) {
	return a.c.GetNode(ctx, id)
}
func (a *liveAPI) ListBackends(ctx context.Context) ([]model.Backend, error) {
	return a.c.ListBackends(ctx)
}
func (a *liveAPI) GetBackend(ctx context.Context, id string) (*model.Backend, error) {
	return a.c.GetBackend(ctx, id)
}
func (a *liveAPI) CreateBackend(ctx context.Context, b model.Backend) (*model.Backend, error) {
	return a.c.CreateBackend(ctx, b)
}
func (a *liveAPI) UpdateBackend(ctx context.Context, b model.Backend) (*model.Backend, error) {
	return a.c.UpdateBackend(ctx, b)
}
func (a *liveAPI) ListTunnels(ctx context.Context) ([]model.Tunnel, error) {
	return a.c.ListTunnels(ctx)
}
func (a *liveAPI) CreateTunnel(ctx context.Context, t model.Tunnel) (*model.Tunnel, error) {
	return a.c.CreateTunnel(ctx, t)
}
func (a *liveAPI) ListMappings(ctx context.Context) ([]model.PortMapping, error) {
	return a.c.ListMappings(ctx)
}
func (a *liveAPI) CreateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error) {
	return a.c.CreateMapping(ctx, m)
}
func (a *liveAPI) UpdateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error) {
	return a.c.UpdateMapping(ctx, m)
}
func (a *liveAPI) PatchMapping(ctx context.Context, id string, enabled bool) (*model.PortMapping, error) {
	return a.c.PatchMapping(ctx, id, enabled)
}
func (a *liveAPI) DeleteMapping(ctx context.Context, id string) error {
	return a.c.DeleteMapping(ctx, id)
}
func (a *liveAPI) ListSniRoutes(ctx context.Context) ([]model.SniRoute, error) {
	return a.c.ListSniRoutes(ctx)
}
func (a *liveAPI) GetSniRoute(ctx context.Context, id string) (*model.SniRoute, error) {
	return a.c.GetSniRoute(ctx, id)
}
func (a *liveAPI) CreateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error) {
	return a.c.CreateSniRoute(ctx, r)
}
func (a *liveAPI) UpdateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error) {
	return a.c.UpdateSniRoute(ctx, r)
}
func (a *liveAPI) DesiredState(ctx context.Context, nodeID string) (*model.DesiredState, error) {
	return a.c.DesiredState(ctx, nodeID)
}
func (a *liveAPI) GetActualState(ctx context.Context, nodeID string) (*model.NodeActualState, error) {
	return a.c.GetActualState(ctx, nodeID)
}
func (a *liveAPI) Apply(ctx context.Context, nodeID string, dryRun bool) (*model.ApplyResult, error) {
	return a.c.Apply(ctx, nodeID, dryRun)
}
func (a *liveAPI) Failback(ctx context.Context, nodeID, backend string) error {
	return a.c.Failback(ctx, nodeID, backend)
}
func (a *liveAPI) ListAudit(ctx context.Context, query string) ([]model.AuditEvent, error) {
	return a.c.ListAudit(ctx, query)
}

type loopback struct {
	h http.Handler
}

func (t *loopback) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL != nil && req.URL.Host == "" && req.Host != "" {
		req = req.Clone(req.Context())
	}
	if req.RemoteAddr == "" {
		req = req.Clone(req.Context())
		req.RemoteAddr = "127.0.0.1:0"
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func queryString(u *url.URL) string {
	if u == nil || u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
