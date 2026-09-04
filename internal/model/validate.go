package model

import (
	"fmt"
	"strings"
	"time"

	"proxyctl/internal/validate"
)

func wrapVal(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimPrefix(err.Error(), "VALIDATION: ")
	return Validation(msg)
}

func (n *Node) Normalize() {
	n.Name = strings.ToLower(strings.TrimSpace(n.Name))
	n.PublicIP = strings.TrimSpace(n.PublicIP)
	if n.Labels == nil {
		n.Labels = map[string]string{}
	}
}

func (n *Node) Validate() error {
	n.Normalize()
	if err := validate.NodeName(n.Name); err != nil {
		return wrapVal(err)
	}
	if err := validate.OptionalIPv4(n.PublicIP); err != nil {
		return Validation("public_ip: " + strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	return nil
}

func (b *Backend) Normalize() {
	b.Name = strings.ToLower(strings.TrimSpace(b.Name))
	b.Address = strings.TrimSpace(b.Address)
}

func (b *Backend) Validate() error {
	b.Normalize()
	if err := validate.BackendName(b.Name); err != nil {
		return wrapVal(err)
	}
	if b.NodeID == "" {
		return Validation("backend node_id is required")
	}
	if err := validate.IPv4(b.Address); err != nil {
		return Validation("address: " + strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	return nil
}

func (t *Tunnel) Normalize() {
	t.Type = TunnelType(strings.ToUpper(strings.TrimSpace(string(t.Type))))
	t.InterfaceName = strings.TrimSpace(t.InterfaceName)
	t.LocalOverlayIP = strings.TrimSpace(t.LocalOverlayIP)
	t.RemoteOverlayIP = strings.TrimSpace(t.RemoteOverlayIP)
	t.Endpoint = strings.TrimSpace(t.Endpoint)
	t.PrivateKeyPath = strings.TrimSpace(t.PrivateKeyPath)
	t.PublicKey = strings.TrimSpace(t.PublicKey)
	t.ServiceName = strings.TrimSpace(t.ServiceName)
	if t.AllowedIPs == nil {
		t.AllowedIPs = []string{}
	}
}

func (t *Tunnel) Validate() error {
	t.Normalize()
	if t.NodeID == "" {
		return Validation("tunnel node_id is required")
	}
	if t.BackendID == "" {
		return Validation("tunnel backend_id is required")
	}
	if err := validate.TunnelType(string(t.Type)); err != nil {
		return wrapVal(err)
	}
	if err := validate.InterfaceName(t.InterfaceName); err != nil {
		return wrapVal(err)
	}
	if err := validate.OverlayPair(t.LocalOverlayIP, t.RemoteOverlayIP); err != nil {
		return wrapVal(err)
	}
	if err := validate.OptionalPort(t.ListenPort); err != nil {
		return wrapVal(err)
	}
	if err := validate.Endpoint(t.Endpoint); err != nil {
		return wrapVal(err)
	}
	for _, cidr := range t.AllowedIPs {
		if err := validate.CIDR(cidr); err != nil {
			return wrapVal(err)
		}
	}
	if t.PersistentKeepalive < 0 || t.PersistentKeepalive > 3600 {
		return Validation("persistent_keepalive must be 0-3600")
	}
	if err := validate.PathRef(t.PrivateKeyPath); err != nil {
		return wrapVal(err)
	}
	if err := validate.ServiceName(t.ServiceName); err != nil {
		return wrapVal(err)
	}
	if t.Type == TunnelWireGuard && t.ListenPort == 0 {
		return Validation("WIREGUARD tunnel requires listen_port")
	}
	if t.Type == TunnelSSHTUN && t.ServiceName == "" {
		return Validation("SSH_TUN tunnel requires service_name (systemd unit)")
	}
	return nil
}

func (m *PortMapping) Normalize() {
	m.Protocol = Protocol(strings.ToUpper(strings.TrimSpace(string(m.Protocol))))
}

func (m *PortMapping) Validate() error {
	m.Normalize()
	if m.NodeID == "" {
		return Validation("mapping node_id is required")
	}
	if m.BackendID == "" {
		return Validation("mapping backend_id is required")
	}
	if err := validate.Protocol(string(m.Protocol)); err != nil {
		return wrapVal(err)
	}
	if err := validate.Port(m.PublicPort); err != nil {
		return Validation("public_port: " + strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	if err := validate.Port(m.BackendPort); err != nil {
		return Validation("backend_port: " + strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	return nil
}

func (r *SniRoute) Normalize() {
	r.Listen = strings.TrimSpace(r.Listen)
	for i := range r.Matches {
		r.Matches[i].Match = strings.ToLower(strings.TrimSpace(r.Matches[i].Match))
		r.Matches[i].Backend = strings.ToLower(strings.TrimSpace(r.Matches[i].Backend))
	}
}

func (r *SniRoute) Validate() error {
	r.Normalize()
	if r.NodeID == "" {
		return Validation("sni_route node_id is required")
	}
	if err := validate.ListenAddr(r.Listen); err != nil {
		return wrapVal(err)
	}
	if len(r.Matches) == 0 {
		return Validation("sni_route requires at least one match")
	}
	defaults := 0
	for i, m := range r.Matches {
		if m.Backend == "" && m.BackendID == "" {
			return Validation(fmt.Sprintf("sni match %d requires backend or backend_id", i))
		}
		if m.Default {
			defaults++
			continue
		}
		if m.Match == "" {
			return Validation(fmt.Sprintf("sni match %d requires match hostname unless default", i))
		}
		if err := validate.Hostname(m.Match); err != nil {
			return wrapVal(err)
		}
	}
	if defaults != 1 {
		return Validation("sni_route requires exactly one default backend")
	}
	return nil
}

func (h *HealthCheck) Normalize() {
	h.Protocol = Protocol(strings.ToUpper(strings.TrimSpace(string(h.Protocol))))
	if h.Interval == 0 {
		h.Interval = 10 * time.Second
	}
	if h.Timeout == 0 {
		h.Timeout = 2 * time.Second
	}
}

func (h *HealthCheck) Validate() error {
	h.Normalize()
	if h.BackendID == "" {
		return Validation("health_check backend_id is required")
	}
	if err := validate.Protocol(string(h.Protocol)); err != nil {
		return wrapVal(err)
	}
	if err := validate.Port(h.Port); err != nil {
		return wrapVal(err)
	}
	if h.Interval < time.Second || h.Interval > time.Minute {
		return Validation("health_check interval must be 1s-60s")
	}
	if h.Timeout < 100*time.Millisecond || h.Timeout >= h.Interval {
		return Validation("health_check timeout must be >=100ms and < interval")
	}
	return nil
}

func (p *FailoverPolicy) ApplyDefaults() {
	d := DefaultFailoverPolicy()
	if p.CheckInterval == 0 {
		p.CheckInterval = d.CheckInterval
	}
	if p.FailureThreshold == 0 {
		p.FailureThreshold = d.FailureThreshold
	}
}

func (p *FailoverPolicy) Validate() error {
	p.ApplyDefaults()
	if p.NodeID == "" {
		return Validation("failover_policy node_id is required")
	}
	if p.BackendID == "" {
		return Validation("failover_policy backend_id is required")
	}
	if p.CheckInterval < time.Second || p.CheckInterval > time.Minute {
		return Validation("check_interval must be 1s-60s")
	}
	if p.FailureThreshold < 1 || p.FailureThreshold > 20 {
		return Validation("failure_threshold must be 1-20")
	}
	return nil
}
