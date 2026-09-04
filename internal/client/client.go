package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"proxyctl/internal/auth"
	"proxyctl/internal/model"
)

type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	UserAgent string
}

func New(baseURL, token string, insecure bool) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit operator flag for lab certs
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     strings.TrimSpace(token),
		HTTP:      &http.Client{Timeout: 30 * time.Second, Transport: tr},
		UserAgent: "proxctl",
	}
}

func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		ts, sig := auth.Sign(c.Token, method, req.URL.Path, body, time.Now())
		req.Header.Set(auth.HeaderTimestamp, ts)
		req.Header.Set(auth.HeaderSignature, sig)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var ce model.CodedError
		if json.Unmarshal(respBody, &ce) == nil && ce.Code != "" {
			ce.HTTP = resp.StatusCode
			return &ce
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) CreateNode(ctx context.Context, n model.Node) (*model.Node, error) {
	var out model.Node
	err := c.Do(ctx, http.MethodPost, "/api/v1/nodes", n, &out)
	return &out, err
}

func (c *Client) ListNodes(ctx context.Context) ([]model.Node, error) {
	var out []model.Node
	err := c.Do(ctx, http.MethodGet, "/api/v1/nodes", nil, &out)
	return out, err
}

func (c *Client) CreateBackend(ctx context.Context, b model.Backend) (*model.Backend, error) {
	var out model.Backend
	err := c.Do(ctx, http.MethodPost, "/api/v1/backends", b, &out)
	return &out, err
}

func (c *Client) ListBackends(ctx context.Context) ([]model.Backend, error) {
	var out []model.Backend
	err := c.Do(ctx, http.MethodGet, "/api/v1/backends", nil, &out)
	return out, err
}

func (c *Client) CreateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error) {
	var out model.PortMapping
	err := c.Do(ctx, http.MethodPost, "/api/v1/mappings", m, &out)
	return &out, err
}

func (c *Client) ListMappings(ctx context.Context) ([]model.PortMapping, error) {
	var out []model.PortMapping
	err := c.Do(ctx, http.MethodGet, "/api/v1/mappings", nil, &out)
	return out, err
}

func (c *Client) DeleteMapping(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/mappings/"+id, nil, nil)
}

func (c *Client) CreateTunnel(ctx context.Context, t model.Tunnel) (*model.Tunnel, error) {
	var out model.Tunnel
	err := c.Do(ctx, http.MethodPost, "/api/v1/tunnels", t, &out)
	return &out, err
}

func (c *Client) ListTunnels(ctx context.Context) ([]model.Tunnel, error) {
	var out []model.Tunnel
	err := c.Do(ctx, http.MethodGet, "/api/v1/tunnels", nil, &out)
	return out, err
}

func (c *Client) TunnelStatus(ctx context.Context, id string) (*model.TunnelStatus, error) {
	var out model.TunnelStatus
	err := c.Do(ctx, http.MethodGet, "/api/v1/tunnels/"+id+"/status", nil, &out)
	return &out, err
}

func (c *Client) CreateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error) {
	var out model.SniRoute
	err := c.Do(ctx, http.MethodPost, "/api/v1/sni-routes", r, &out)
	return &out, err
}

func (c *Client) DesiredState(ctx context.Context, nodeID string) (*model.DesiredState, error) {
	var out model.DesiredState
	err := c.Do(ctx, http.MethodGet, "/api/v1/nodes/"+nodeID+"/desired-state", nil, &out)
	return &out, err
}

func (c *Client) PutActualState(ctx context.Context, nodeID string, st model.ActualState) error {
	return c.Do(ctx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/actual-state", st, nil)
}

func (c *Client) Apply(ctx context.Context, nodeID string, dryRun bool) (*model.ApplyResult, error) {
	var out model.ApplyResult
	err := c.Do(ctx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/apply", model.ApplyRequest{DryRun: dryRun}, &out)
	return &out, err
}

func (c *Client) Failback(ctx context.Context, nodeID, backend string) error {
	return c.Do(ctx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/failback", map[string]any{"backend": backend, "action": "fail_forward"}, nil)
}

func (c *Client) Healthz(ctx context.Context) error {
	return c.Do(ctx, http.MethodGet, "/healthz", nil, nil)
}

func (c *Client) Whoami(ctx context.Context) (*model.PrincipalView, error) {
	var out model.PrincipalView
	err := c.Do(ctx, http.MethodGet, "/api/v1/whoami", nil, &out)
	return &out, err
}

func (c *Client) CreateToken(ctx context.Context, req model.TokenCreateRequest) (*model.TokenCreateResult, error) {
	var out model.TokenCreateResult
	err := c.Do(ctx, http.MethodPost, "/api/v1/tokens", req, &out)
	return &out, err
}

func (c *Client) GetNode(ctx context.Context, id string) (*model.Node, error) {
	var out model.Node
	err := c.Do(ctx, http.MethodGet, "/api/v1/nodes/"+id, nil, &out)
	return &out, err
}

func (c *Client) GetBackend(ctx context.Context, id string) (*model.Backend, error) {
	var out model.Backend
	err := c.Do(ctx, http.MethodGet, "/api/v1/backends/"+id, nil, &out)
	return &out, err
}

func (c *Client) UpdateBackend(ctx context.Context, b model.Backend) (*model.Backend, error) {
	var out model.Backend
	err := c.Do(ctx, http.MethodPut, "/api/v1/backends/"+string(b.ID), b, &out)
	return &out, err
}

func (c *Client) GetMapping(ctx context.Context, id string) (*model.PortMapping, error) {
	var out model.PortMapping
	err := c.Do(ctx, http.MethodGet, "/api/v1/mappings/"+id, nil, &out)
	return &out, err
}

func (c *Client) UpdateMapping(ctx context.Context, m model.PortMapping) (*model.PortMapping, error) {
	var out model.PortMapping
	err := c.Do(ctx, http.MethodPut, "/api/v1/mappings/"+string(m.ID), m, &out)
	return &out, err
}

func (c *Client) PatchMapping(ctx context.Context, id string, enabled bool) (*model.PortMapping, error) {
	var out model.PortMapping
	err := c.Do(ctx, http.MethodPatch, "/api/v1/mappings/"+id, model.MappingPatch{Enabled: &enabled}, &out)
	return &out, err
}

func (c *Client) ListSniRoutes(ctx context.Context) ([]model.SniRoute, error) {
	var out []model.SniRoute
	err := c.Do(ctx, http.MethodGet, "/api/v1/sni-routes", nil, &out)
	return out, err
}

func (c *Client) GetSniRoute(ctx context.Context, id string) (*model.SniRoute, error) {
	var out model.SniRoute
	err := c.Do(ctx, http.MethodGet, "/api/v1/sni-routes/"+id, nil, &out)
	return &out, err
}

func (c *Client) UpdateSniRoute(ctx context.Context, r model.SniRoute) (*model.SniRoute, error) {
	var out model.SniRoute
	err := c.Do(ctx, http.MethodPut, "/api/v1/sni-routes/"+string(r.ID), r, &out)
	return &out, err
}

func (c *Client) GetActualState(ctx context.Context, nodeID string) (*model.NodeActualState, error) {
	var out model.NodeActualState
	err := c.Do(ctx, http.MethodGet, "/api/v1/nodes/"+nodeID+"/actual-state", nil, &out)
	return &out, err
}

func (c *Client) ListAudit(ctx context.Context, query string) ([]model.AuditEvent, error) {
	path := "/api/v1/audit"
	if strings.TrimSpace(query) != "" {
		if !strings.HasPrefix(query, "?") {
			path += "?"
		}
		path += query
	}
	var out []model.AuditEvent
	err := c.Do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}
