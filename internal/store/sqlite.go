package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"proxyctl/internal/model"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil && !os.IsExist(err) {
		if filepath.Dir(path) != "." && filepath.Dir(path) != "" {
			return nil, fmt.Errorf("mkdir data dir: %w", err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) CreateNode(ctx context.Context, n *model.Node) error {
	labels, _ := json.Marshal(n.Labels)
	_, err := s.db.ExecContext(ctx, `INSERT INTO nodes(id,name,public_ip,labels_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		string(n.ID), n.Name, n.PublicIP, string(labels), nowRFC3339(n.CreatedAt), nowRFC3339(n.UpdatedAt))
	if isUnique(err) {
		return model.ErrConflict("node name already exists")
	}
	return wrap("CreateNode", err)
}

func (s *SQLite) GetNode(ctx context.Context, id model.ID) (*model.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT id,name,public_ip,labels_json,created_at,updated_at FROM nodes WHERE id=?`, string(id)))
}

func (s *SQLite) GetNodeByName(ctx context.Context, name string) (*model.Node, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT id,name,public_ip,labels_json,created_at,updated_at FROM nodes WHERE name=?`, name))
}

func (s *SQLite) ListNodes(ctx context.Context) ([]model.Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,public_ip,labels_json,created_at,updated_at FROM nodes ORDER BY name`)
	if err != nil {
		return nil, wrap("ListNodes", err)
	}
	defer rows.Close()
	var out []model.Node
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (s *SQLite) CreateBackend(ctx context.Context, b *model.Backend) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO backends(id,name,node_id,address,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		string(b.ID), b.Name, string(b.NodeID), b.Address, nowRFC3339(b.CreatedAt), nowRFC3339(b.UpdatedAt))
	if isUnique(err) {
		return model.ErrConflict("backend name already exists")
	}
	if isFK(err) {
		return model.NotFound("node", string(b.NodeID))
	}
	return wrap("CreateBackend", err)
}

func (s *SQLite) GetBackend(ctx context.Context, id model.ID) (*model.Backend, error) {
	return scanBackend(s.db.QueryRowContext(ctx, `SELECT id,name,node_id,address,created_at,updated_at FROM backends WHERE id=?`, string(id)))
}

func (s *SQLite) GetBackendByName(ctx context.Context, name string) (*model.Backend, error) {
	return scanBackend(s.db.QueryRowContext(ctx, `SELECT id,name,node_id,address,created_at,updated_at FROM backends WHERE name=?`, name))
}

func (s *SQLite) ListBackends(ctx context.Context) ([]model.Backend, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,node_id,address,created_at,updated_at FROM backends ORDER BY name`)
	if err != nil {
		return nil, wrap("ListBackends", err)
	}
	defer rows.Close()
	var out []model.Backend
	for rows.Next() {
		b, err := scanBackendRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *SQLite) ListBackendsByNode(ctx context.Context, nodeID model.ID) ([]model.Backend, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,node_id,address,created_at,updated_at FROM backends WHERE node_id=? ORDER BY name`, string(nodeID))
	if err != nil {
		return nil, wrap("ListBackendsByNode", err)
	}
	defer rows.Close()
	var out []model.Backend
	for rows.Next() {
		b, err := scanBackendRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateBackend(ctx context.Context, b *model.Backend) error {
	res, err := s.db.ExecContext(ctx, `UPDATE backends SET name=?, node_id=?, address=?, updated_at=? WHERE id=?`,
		b.Name, string(b.NodeID), b.Address, nowRFC3339(b.UpdatedAt), string(b.ID))
	if isUnique(err) {
		return model.ErrConflict("backend name already exists")
	}
	if isFK(err) {
		return model.NotFound("node", string(b.NodeID))
	}
	if err != nil {
		return wrap("UpdateBackend", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) CreateTunnel(ctx context.Context, t *model.Tunnel) error {
	ips, _ := json.Marshal(t.AllowedIPs)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tunnels(id,node_id,backend_id,type,interface_name,local_overlay_ip,remote_overlay_ip,listen_port,endpoint,allowed_ips_json,persistent_keepalive,priority,private_key_path,public_key,service_name,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(t.ID), string(t.NodeID), string(t.BackendID), string(t.Type), t.InterfaceName, t.LocalOverlayIP, t.RemoteOverlayIP,
		t.ListenPort, t.Endpoint, string(ips), t.PersistentKeepalive, t.Priority, t.PrivateKeyPath, t.PublicKey, t.ServiceName,
		nowRFC3339(t.CreatedAt), nowRFC3339(t.UpdatedAt))
	if isFK(err) {
		return model.NotFound("node or backend", string(t.NodeID)+"/"+string(t.BackendID))
	}
	return wrap("CreateTunnel", err)
}

func (s *SQLite) GetTunnel(ctx context.Context, id model.ID) (*model.Tunnel, error) {
	return scanTunnel(s.db.QueryRowContext(ctx, tunnelCols+` FROM tunnels WHERE id=?`, string(id)))
}

func (s *SQLite) ListTunnels(ctx context.Context) ([]model.Tunnel, error) {
	rows, err := s.db.QueryContext(ctx, tunnelCols+` FROM tunnels ORDER BY priority, interface_name`)
	if err != nil {
		return nil, wrap("ListTunnels", err)
	}
	defer rows.Close()
	var out []model.Tunnel
	for rows.Next() {
		t, err := scanTunnelRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLite) ListTunnelsByNode(ctx context.Context, nodeID model.ID) ([]model.Tunnel, error) {
	rows, err := s.db.QueryContext(ctx, tunnelCols+` FROM tunnels WHERE node_id=? ORDER BY priority, interface_name`, string(nodeID))
	if err != nil {
		return nil, wrap("ListTunnelsByNode", err)
	}
	defer rows.Close()
	var out []model.Tunnel
	for rows.Next() {
		t, err := scanTunnelRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLite) CreateMapping(ctx context.Context, m *model.PortMapping) error {
	m.Enabled = true
	_, err := s.db.ExecContext(ctx, `INSERT INTO port_mappings(id,node_id,backend_id,protocol,public_port,backend_port,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		string(m.ID), string(m.NodeID), string(m.BackendID), string(m.Protocol), m.PublicPort, m.BackendPort, 1, nowRFC3339(m.CreatedAt), nowRFC3339(m.UpdatedAt))
	if isUnique(err) {
		return model.ErrConflict(fmt.Sprintf("mapping %s/%d already exists on node", m.Protocol, m.PublicPort))
	}
	if isFK(err) {
		return model.NotFound("node or backend", string(m.NodeID))
	}
	return wrap("CreateMapping", err)
}

func (s *SQLite) GetMapping(ctx context.Context, id model.ID) (*model.PortMapping, error) {
	return scanMapping(s.db.QueryRowContext(ctx, mappingCols+` FROM port_mappings WHERE id=?`, string(id)))
}

func (s *SQLite) ListMappings(ctx context.Context) ([]model.PortMapping, error) {
	rows, err := s.db.QueryContext(ctx, mappingCols+` FROM port_mappings ORDER BY public_port`)
	if err != nil {
		return nil, wrap("ListMappings", err)
	}
	defer rows.Close()
	var out []model.PortMapping
	for rows.Next() {
		m, err := scanMappingRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *SQLite) ListMappingsByNode(ctx context.Context, nodeID model.ID) ([]model.PortMapping, error) {
	rows, err := s.db.QueryContext(ctx, mappingCols+` FROM port_mappings WHERE node_id=? ORDER BY public_port`, string(nodeID))
	if err != nil {
		return nil, wrap("ListMappingsByNode", err)
	}
	defer rows.Close()
	var out []model.PortMapping
	for rows.Next() {
		m, err := scanMappingRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateMapping(ctx context.Context, m *model.PortMapping) error {
	res, err := s.db.ExecContext(ctx, `UPDATE port_mappings SET node_id=?, backend_id=?, protocol=?, public_port=?, backend_port=?, enabled=?, updated_at=? WHERE id=?`,
		string(m.NodeID), string(m.BackendID), string(m.Protocol), m.PublicPort, m.BackendPort, boolInt(m.Enabled), nowRFC3339(m.UpdatedAt), string(m.ID))
	if isUnique(err) {
		return model.ErrConflict(fmt.Sprintf("mapping %s/%d already exists on node", m.Protocol, m.PublicPort))
	}
	if isFK(err) {
		return model.NotFound("node or backend", string(m.NodeID))
	}
	if err != nil {
		return wrap("UpdateMapping", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) DeleteMapping(ctx context.Context, id model.ID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM port_mappings WHERE id=?`, string(id))
	if err != nil {
		return wrap("DeleteMapping", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) CreateSniRoute(ctx context.Context, r *model.SniRoute) error {
	matches, _ := json.Marshal(r.Matches)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sni_routes(id,node_id,listen,matches_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		string(r.ID), string(r.NodeID), r.Listen, string(matches), nowRFC3339(r.CreatedAt), nowRFC3339(r.UpdatedAt))
	if isFK(err) {
		return model.NotFound("node", string(r.NodeID))
	}
	return wrap("CreateSniRoute", err)
}

func (s *SQLite) GetSniRoute(ctx context.Context, id model.ID) (*model.SniRoute, error) {
	return scanSni(s.db.QueryRowContext(ctx, `SELECT id,node_id,listen,matches_json,created_at,updated_at FROM sni_routes WHERE id=?`, string(id)))
}

func (s *SQLite) ListSniRoutes(ctx context.Context) ([]model.SniRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,node_id,listen,matches_json,created_at,updated_at FROM sni_routes ORDER BY listen`)
	if err != nil {
		return nil, wrap("ListSniRoutes", err)
	}
	defer rows.Close()
	var out []model.SniRoute
	for rows.Next() {
		r, err := scanSniRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateSniRoute(ctx context.Context, r *model.SniRoute) error {
	matches, _ := json.Marshal(r.Matches)
	res, err := s.db.ExecContext(ctx, `UPDATE sni_routes SET node_id=?, listen=?, matches_json=?, updated_at=? WHERE id=?`,
		string(r.NodeID), r.Listen, string(matches), nowRFC3339(r.UpdatedAt), string(r.ID))
	if isFK(err) {
		return model.NotFound("node", string(r.NodeID))
	}
	if err != nil {
		return wrap("UpdateSniRoute", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) ListSniRoutesByNode(ctx context.Context, nodeID model.ID) ([]model.SniRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,node_id,listen,matches_json,created_at,updated_at FROM sni_routes WHERE node_id=?`, string(nodeID))
	if err != nil {
		return nil, wrap("ListSniRoutesByNode", err)
	}
	defer rows.Close()
	var out []model.SniRoute
	for rows.Next() {
		r, err := scanSniRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *SQLite) CreateHealthCheck(ctx context.Context, h *model.HealthCheck) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO health_checks(id,backend_id,protocol,port,interval_ms,timeout_ms,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		string(h.ID), string(h.BackendID), string(h.Protocol), h.Port, h.Interval.Milliseconds(), h.Timeout.Milliseconds(), nowRFC3339(h.CreatedAt), nowRFC3339(h.UpdatedAt))
	if isFK(err) {
		return model.NotFound("backend", string(h.BackendID))
	}
	return wrap("CreateHealthCheck", err)
}

func (s *SQLite) ListHealthChecksByBackend(ctx context.Context, backendID model.ID) ([]model.HealthCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,backend_id,protocol,port,interval_ms,timeout_ms,created_at,updated_at FROM health_checks WHERE backend_id=?`, string(backendID))
	if err != nil {
		return nil, wrap("ListHealthChecksByBackend", err)
	}
	defer rows.Close()
	var out []model.HealthCheck
	for rows.Next() {
		h, err := scanHCRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *SQLite) CreateFailoverPolicy(ctx context.Context, p *model.FailoverPolicy) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO failover_policies(id,node_id,backend_id,automatic_failback,automatic_failforward,check_interval_ms,failure_threshold,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		string(p.ID), string(p.NodeID), string(p.BackendID), boolInt(p.AutomaticFailback), boolInt(p.AutomaticFailforward),
		p.CheckInterval.Milliseconds(), p.FailureThreshold, nowRFC3339(p.CreatedAt), nowRFC3339(p.UpdatedAt))
	if isUnique(err) {
		return model.ErrConflict("failover policy already exists for this node/backend")
	}
	if isFK(err) {
		return model.NotFound("node or backend", string(p.NodeID))
	}
	return wrap("CreateFailoverPolicy", err)
}

func (s *SQLite) GetFailoverPolicy(ctx context.Context, nodeID, backendID model.ID) (*model.FailoverPolicy, error) {
	return scanFO(s.db.QueryRowContext(ctx, `SELECT id,node_id,backend_id,automatic_failback,automatic_failforward,check_interval_ms,failure_threshold,created_at,updated_at FROM failover_policies WHERE node_id=? AND backend_id=?`, string(nodeID), string(backendID)))
}

func (s *SQLite) ListFailoverPoliciesByNode(ctx context.Context, nodeID model.ID) ([]model.FailoverPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,node_id,backend_id,automatic_failback,automatic_failforward,check_interval_ms,failure_threshold,created_at,updated_at FROM failover_policies WHERE node_id=?`, string(nodeID))
	if err != nil {
		return nil, wrap("ListFailoverPoliciesByNode", err)
	}
	defer rows.Close()
	var out []model.FailoverPolicy
	for rows.Next() {
		p, err := scanFORow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *SQLite) DesiredState(ctx context.Context, nodeID model.ID) (*model.DesiredState, error) {
	n, err := s.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	ds := &model.DesiredState{Node: *n}
	if ds.Backends, err = s.ListBackendsByNode(ctx, nodeID); err != nil {
		return nil, err
	}
	if ds.Tunnels, err = s.ListTunnelsByNode(ctx, nodeID); err != nil {
		return nil, err
	}
	if ds.Mappings, err = s.ListMappingsByNode(ctx, nodeID); err != nil {
		return nil, err
	}
	ds.Mappings = filterEnabledMappings(ds.Mappings)
	if ds.SniRoutes, err = s.ListSniRoutesByNode(ctx, nodeID); err != nil {
		return nil, err
	}
	if ds.FailoverPolicies, err = s.ListFailoverPoliciesByNode(ctx, nodeID); err != nil {
		return nil, err
	}
	for _, b := range ds.Backends {
		hcs, err := s.ListHealthChecksByBackend(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		ds.HealthChecks = append(ds.HealthChecks, hcs...)
	}
	if ds.Backends == nil {
		ds.Backends = []model.Backend{}
	}
	if ds.Tunnels == nil {
		ds.Tunnels = []model.Tunnel{}
	}
	if ds.Mappings == nil {
		ds.Mappings = []model.PortMapping{}
	}
	if ds.SniRoutes == nil {
		ds.SniRoutes = []model.SniRoute{}
	}
	if ds.HealthChecks == nil {
		ds.HealthChecks = []model.HealthCheck{}
	}
	if ds.FailoverPolicies == nil {
		ds.FailoverPolicies = []model.FailoverPolicy{}
	}
	if ds.FailbackIntents, err = s.ListFailbackIntents(ctx, nodeID); err != nil {
		return nil, err
	}
	if ds.FailbackIntents == nil {
		ds.FailbackIntents = []model.FailbackIntent{}
	}
	return ds, nil
}

func (s *SQLite) PutActualState(ctx context.Context, nodeID model.ID, st model.ActualState, status model.AgentStatus) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	now := nowRFC3339(time.Now())
	lr := nowRFC3339(status.LastReconcile)
	lh := nowRFC3339(status.LastHeartbeat)
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_status(node_id,healthy,last_reconcile,last_heartbeat,version,actual_state_json,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET healthy=excluded.healthy,last_reconcile=excluded.last_reconcile,last_heartbeat=excluded.last_heartbeat,version=excluded.version,actual_state_json=excluded.actual_state_json,updated_at=excluded.updated_at`,
		string(nodeID), boolInt(status.Healthy), lr, lh, status.Version, string(body), now)
	if isFK(err) {
		return model.NotFound("node", string(nodeID))
	}
	return wrap("PutActualState", err)
}

func (s *SQLite) GetActualState(ctx context.Context, nodeID model.ID) (*model.ActualState, *model.AgentStatus, error) {
	var healthy int
	var lastRec, lastHB, ver, body, updated string
	err := s.db.QueryRowContext(ctx, `SELECT healthy,last_reconcile,last_heartbeat,version,actual_state_json,updated_at FROM agent_status WHERE node_id=?`, string(nodeID)).
		Scan(&healthy, &lastRec, &lastHB, &ver, &body, &updated)
	if err != nil {
		return nil, nil, wrap("GetActualState", err)
	}
	st := &model.ActualState{}
	if body != "" && body != "{}" {
		if err := json.Unmarshal([]byte(body), st); err != nil {
			return nil, nil, err
		}
	}
	status := &model.AgentStatus{
		NodeID:        nodeID,
		Healthy:       healthy == 1,
		LastReconcile: parseTime(lastRec),
		LastHeartbeat: parseTime(lastHB),
		Version:       ver,
	}
	return st, status, nil
}

func (s *SQLite) CreateToken(ctx context.Context, t *model.Token) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens(id,name,hash,role,created_at,revoked) VALUES(?,?,?,?,?,?)`,
		string(t.ID), t.Name, t.Hash, string(t.Role), nowRFC3339(t.CreatedAt), boolInt(t.Revoked))
	if isUnique(err) {
		return model.ErrConflict("token name already exists")
	}
	return wrap("CreateToken", err)
}

func (s *SQLite) LookupTokenByHash(ctx context.Context, hash string) (*model.Token, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,hash,role,created_at,revoked FROM tokens WHERE hash=? AND revoked=0`, hash)
	return scanToken(row)
}

func (s *SQLite) ListTokens(ctx context.Context) ([]model.Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,hash,role,created_at,revoked FROM tokens`)
	if err != nil {
		return nil, wrap("ListTokens", err)
	}
	defer rows.Close()
	var out []model.Token
	for rows.Next() {
		t, err := scanTokenRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLite) HasTokenName(ctx context.Context, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM tokens WHERE name=?`, name).Scan(&n)
	return n > 0, err
}

func (s *SQLite) AppendAudit(ctx context.Context, e *model.AuditEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(id,timestamp,actor,action,resource,resource_id,detail,success) VALUES(?,?,?,?,?,?,?,?)`,
		string(e.ID), nowRFC3339(e.Timestamp), e.Actor, e.Action, e.Resource, e.ResourceID, e.Detail, boolInt(e.Success))
	return wrap("AppendAudit", err)
}

func (s *SQLite) ListAudit(ctx context.Context, limit int) ([]model.AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,timestamp,actor,action,resource,resource_id,detail,success FROM audit_events ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrap("ListAudit", err)
	}
	defer rows.Close()
	var out []model.AuditEvent
	for rows.Next() {
		var e model.AuditEvent
		var ts string
		var success int
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.Resource, &e.ResourceID, &e.Detail, &success); err != nil {
			return nil, err
		}
		e.Timestamp = parseTime(ts)
		e.Success = success == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) CreateFailbackIntent(ctx context.Context, in *model.FailbackIntent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO failback_intents(id,node_id,backend_id,action,created_at) VALUES(?,?,?,?,?)`,
		string(in.ID), string(in.NodeID), string(in.BackendID), in.Action, nowRFC3339(in.CreatedAt))
	if isFK(err) {
		return model.NotFound("node or backend", string(in.NodeID))
	}
	return wrap("CreateFailbackIntent", err)
}

func (s *SQLite) ListFailbackIntents(ctx context.Context, nodeID model.ID) ([]model.FailbackIntent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,node_id,backend_id,action,created_at FROM failback_intents WHERE node_id=? ORDER BY created_at`, string(nodeID))
	if err != nil {
		return nil, wrap("ListFailbackIntents", err)
	}
	defer rows.Close()
	var out []model.FailbackIntent
	for rows.Next() {
		var in model.FailbackIntent
		var ts string
		if err := rows.Scan(&in.ID, &in.NodeID, &in.BackendID, &in.Action, &ts); err != nil {
			return nil, err
		}
		in.CreatedAt = parseTime(ts)
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteFailbackIntent(ctx context.Context, id model.ID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM failback_intents WHERE id=?`, string(id))
	if err != nil {
		return wrap("DeleteFailbackIntent", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique constraint")
}

func isFK(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "foreign key")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const tunnelCols = `SELECT id,node_id,backend_id,type,interface_name,local_overlay_ip,remote_overlay_ip,listen_port,endpoint,allowed_ips_json,persistent_keepalive,priority,private_key_path,public_key,service_name,created_at,updated_at`

const mappingCols = `SELECT id,node_id,backend_id,protocol,public_port,backend_port,enabled,created_at,updated_at`

func filterEnabledMappings(in []model.PortMapping) []model.PortMapping {
	out := make([]model.PortMapping, 0, len(in))
	for _, m := range in {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}
