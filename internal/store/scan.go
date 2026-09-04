package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"proxyctl/internal/model"
)

type scanner interface {
	Scan(dest ...any) error
}

func scanNode(row scanner) (*model.Node, error) {
	n, err := scanNodeInto(row)
	if err != nil {
		return nil, wrap("GetNode", err)
	}
	return n, nil
}

func scanNodeRow(rows *sql.Rows) (*model.Node, error) {
	return scanNodeInto(rows)
}

func scanNodeInto(row scanner) (*model.Node, error) {
	var n model.Node
	var labels, created, updated string
	if err := row.Scan(&n.ID, &n.Name, &n.PublicIP, &labels, &created, &updated); err != nil {
		return nil, err
	}
	n.Labels = map[string]string{}
	if labels != "" {
		_ = json.Unmarshal([]byte(labels), &n.Labels)
	}
	n.CreatedAt = parseTime(created)
	n.UpdatedAt = parseTime(updated)
	return &n, nil
}

func scanBackend(row scanner) (*model.Backend, error) {
	b, err := scanBackendInto(row)
	if err != nil {
		return nil, wrap("GetBackend", err)
	}
	return b, nil
}

func scanBackendRow(rows *sql.Rows) (*model.Backend, error) {
	return scanBackendInto(rows)
}

func scanBackendInto(row scanner) (*model.Backend, error) {
	var b model.Backend
	var created, updated string
	if err := row.Scan(&b.ID, &b.Name, &b.NodeID, &b.Address, &created, &updated); err != nil {
		return nil, err
	}
	b.CreatedAt = parseTime(created)
	b.UpdatedAt = parseTime(updated)
	return &b, nil
}

func scanTunnel(row scanner) (*model.Tunnel, error) {
	t, err := scanTunnelInto(row)
	if err != nil {
		return nil, wrap("GetTunnel", err)
	}
	return t, nil
}

func scanTunnelRow(rows *sql.Rows) (*model.Tunnel, error) {
	return scanTunnelInto(rows)
}

func scanTunnelInto(row scanner) (*model.Tunnel, error) {
	var t model.Tunnel
	var ips, created, updated string
	if err := row.Scan(&t.ID, &t.NodeID, &t.BackendID, &t.Type, &t.InterfaceName, &t.LocalOverlayIP, &t.RemoteOverlayIP,
		&t.ListenPort, &t.Endpoint, &ips, &t.PersistentKeepalive, &t.Priority, &t.PrivateKeyPath, &t.PublicKey, &t.ServiceName,
		&created, &updated); err != nil {
		return nil, err
	}
	if ips != "" {
		_ = json.Unmarshal([]byte(ips), &t.AllowedIPs)
	}
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	return &t, nil
}

func scanMapping(row scanner) (*model.PortMapping, error) {
	m, err := scanMappingInto(row)
	if err != nil {
		return nil, wrap("GetMapping", err)
	}
	return m, nil
}

func scanMappingRow(rows *sql.Rows) (*model.PortMapping, error) {
	return scanMappingInto(rows)
}

func scanMappingInto(row scanner) (*model.PortMapping, error) {
	var m model.PortMapping
	var created, updated string
	var enabled int
	if err := row.Scan(&m.ID, &m.NodeID, &m.BackendID, &m.Protocol, &m.PublicPort, &m.BackendPort, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	m.Enabled = enabled == 1
	m.CreatedAt = parseTime(created)
	m.UpdatedAt = parseTime(updated)
	return &m, nil
}

func scanSni(row scanner) (*model.SniRoute, error) {
	r, err := scanSniInto(row)
	if err != nil {
		return nil, wrap("GetSniRoute", err)
	}
	return r, nil
}

func scanSniRow(rows *sql.Rows) (*model.SniRoute, error) {
	return scanSniInto(rows)
}

func scanSniInto(row scanner) (*model.SniRoute, error) {
	var r model.SniRoute
	var matches, created, updated string
	if err := row.Scan(&r.ID, &r.NodeID, &r.Listen, &matches, &created, &updated); err != nil {
		return nil, err
	}
	if matches != "" {
		_ = json.Unmarshal([]byte(matches), &r.Matches)
	}
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(updated)
	return &r, nil
}

func scanHCRow(rows *sql.Rows) (model.HealthCheck, error) {
	var h model.HealthCheck
	var created, updated string
	var intervalMS, timeoutMS int64
	if err := rows.Scan(&h.ID, &h.BackendID, &h.Protocol, &h.Port, &intervalMS, &timeoutMS, &created, &updated); err != nil {
		return h, err
	}
	h.Interval = time.Duration(intervalMS) * time.Millisecond
	h.Timeout = time.Duration(timeoutMS) * time.Millisecond
	h.CreatedAt = parseTime(created)
	h.UpdatedAt = parseTime(updated)
	return h, nil
}

func scanFO(row scanner) (*model.FailoverPolicy, error) {
	p, err := scanFOInto(row)
	if err != nil {
		return nil, wrap("GetFailoverPolicy", err)
	}
	return p, nil
}

func scanFORow(rows *sql.Rows) (*model.FailoverPolicy, error) {
	return scanFOInto(rows)
}

func scanFOInto(row scanner) (*model.FailoverPolicy, error) {
	var p model.FailoverPolicy
	var fb, ff int
	var intervalMS int64
	var created, updated string
	if err := row.Scan(&p.ID, &p.NodeID, &p.BackendID, &fb, &ff, &intervalMS, &p.FailureThreshold, &created, &updated); err != nil {
		return nil, err
	}
	p.AutomaticFailback = fb == 1
	p.AutomaticFailforward = ff == 1
	p.CheckInterval = time.Duration(intervalMS) * time.Millisecond
	p.CreatedAt = parseTime(created)
	p.UpdatedAt = parseTime(updated)
	return &p, nil
}

func scanToken(row scanner) (*model.Token, error) {
	t, err := scanTokenInto(row)
	if err != nil {
		return nil, wrap("LookupToken", err)
	}
	return t, nil
}

func scanTokenRow(rows *sql.Rows) (*model.Token, error) {
	return scanTokenInto(rows)
}

func scanTokenInto(row scanner) (*model.Token, error) {
	var t model.Token
	var created string
	var revoked int
	if err := row.Scan(&t.ID, &t.Name, &t.Hash, &t.Role, &created, &revoked); err != nil {
		return nil, err
	}
	t.CreatedAt = parseTime(created)
	t.Revoked = revoked == 1
	return &t, nil
}
