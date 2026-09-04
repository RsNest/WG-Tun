package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"proxyctl/internal/model"
)

// Store is the persistence interface. SQLite is the MVP backend; a later
// Postgres implementation can satisfy the same methods.
type Store interface {
	Ping(ctx context.Context) error
	Close() error

	CreateNode(ctx context.Context, n *model.Node) error
	GetNode(ctx context.Context, id model.ID) (*model.Node, error)
	GetNodeByName(ctx context.Context, name string) (*model.Node, error)
	ListNodes(ctx context.Context) ([]model.Node, error)

	CreateBackend(ctx context.Context, b *model.Backend) error
	GetBackend(ctx context.Context, id model.ID) (*model.Backend, error)
	GetBackendByName(ctx context.Context, name string) (*model.Backend, error)
	ListBackends(ctx context.Context) ([]model.Backend, error)
	ListBackendsByNode(ctx context.Context, nodeID model.ID) ([]model.Backend, error)
	UpdateBackend(ctx context.Context, b *model.Backend) error

	CreateTunnel(ctx context.Context, t *model.Tunnel) error
	GetTunnel(ctx context.Context, id model.ID) (*model.Tunnel, error)
	ListTunnels(ctx context.Context) ([]model.Tunnel, error)
	ListTunnelsByNode(ctx context.Context, nodeID model.ID) ([]model.Tunnel, error)

	CreateMapping(ctx context.Context, m *model.PortMapping) error
	GetMapping(ctx context.Context, id model.ID) (*model.PortMapping, error)
	ListMappings(ctx context.Context) ([]model.PortMapping, error)
	ListMappingsByNode(ctx context.Context, nodeID model.ID) ([]model.PortMapping, error)
	UpdateMapping(ctx context.Context, m *model.PortMapping) error
	DeleteMapping(ctx context.Context, id model.ID) error

	CreateSniRoute(ctx context.Context, r *model.SniRoute) error
	GetSniRoute(ctx context.Context, id model.ID) (*model.SniRoute, error)
	ListSniRoutes(ctx context.Context) ([]model.SniRoute, error)
	ListSniRoutesByNode(ctx context.Context, nodeID model.ID) ([]model.SniRoute, error)
	UpdateSniRoute(ctx context.Context, r *model.SniRoute) error

	CreateHealthCheck(ctx context.Context, h *model.HealthCheck) error
	ListHealthChecksByBackend(ctx context.Context, backendID model.ID) ([]model.HealthCheck, error)

	CreateFailoverPolicy(ctx context.Context, p *model.FailoverPolicy) error
	GetFailoverPolicy(ctx context.Context, nodeID, backendID model.ID) (*model.FailoverPolicy, error)
	ListFailoverPoliciesByNode(ctx context.Context, nodeID model.ID) ([]model.FailoverPolicy, error)

	DesiredState(ctx context.Context, nodeID model.ID) (*model.DesiredState, error)
	PutActualState(ctx context.Context, nodeID model.ID, st model.ActualState, status model.AgentStatus) error
	GetActualState(ctx context.Context, nodeID model.ID) (*model.ActualState, *model.AgentStatus, error)

	CreateToken(ctx context.Context, t *model.Token) error
	LookupTokenByHash(ctx context.Context, hash string) (*model.Token, error)
	ListTokens(ctx context.Context) ([]model.Token, error)
	HasTokenName(ctx context.Context, name string) (bool, error)

	AppendAudit(ctx context.Context, e *model.AuditEvent) error
	ListAudit(ctx context.Context, limit int) ([]model.AuditEvent, error)

	CreateFailbackIntent(ctx context.Context, in *model.FailbackIntent) error
	ListFailbackIntents(ctx context.Context, nodeID model.ID) ([]model.FailbackIntent, error)
	DeleteFailbackIntent(ctx context.Context, id model.ID) error
}

var ErrNotFound = errors.New("not found")

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

func nowRFC3339(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, s)
		if err2 != nil {
			return time.Time{}
		}
		return t2
	}
	return t
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
