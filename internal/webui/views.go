package webui

import (
	"strings"
	"time"

	"proxyctl/internal/model"
)

type Catalog struct {
	nodes    map[model.ID]model.Node
	backends map[model.ID]model.Backend
}

func newCatalog(nodes []model.Node, backends []model.Backend) Catalog {
	c := Catalog{
		nodes:    map[model.ID]model.Node{},
		backends: map[model.ID]model.Backend{},
	}
	for _, n := range nodes {
		c.nodes[n.ID] = n
	}
	for _, b := range backends {
		c.backends[b.ID] = b
	}
	return c
}

func (c Catalog) NodeName(id model.ID) string {
	if n, ok := c.nodes[id]; ok {
		return n.Name
	}
	if id == "" {
		return "—"
	}
	return string(id)
}

func (c Catalog) BackendName(id model.ID) string {
	if b, ok := c.backends[id]; ok {
		return b.Name
	}
	if id == "" {
		return "—"
	}
	return string(id)
}

type nodeCard struct {
	ID            string
	Name          string
	PublicIP      string
	Status        string
	StatusClass   string
	Transport     string
	Handshake     string
	LastReconcile string
	Heartbeat     string
	MappingCount  int
}

func buildNodeCard(now time.Time, n model.Node, mappings []model.PortMapping, runtime *model.NodeActualState) nodeCard {
	card := nodeCard{
		ID:            string(n.ID),
		Name:          n.Name,
		PublicIP:      n.PublicIP,
		Status:        "unknown",
		StatusClass:   "status-unknown",
		Transport:     "—",
		Handshake:     "—",
		LastReconcile: "never",
		Heartbeat:     "never",
	}
	for _, m := range mappings {
		if m.NodeID == n.ID && m.Enabled {
			card.MappingCount++
		}
	}
	if runtime == nil || runtime.Status == nil || runtime.Status.LastHeartbeat.IsZero() {
		card.Status = "unhealthy"
		card.StatusClass = "status-unhealthy"
		return card
	}
	st := runtime.Status
	card.Heartbeat = relTime(now, st.LastHeartbeat)
	card.LastReconcile = relTime(now, st.LastReconcile)
	card.Transport = pickTransport(st.TransportStates)
	if runtime.Actual != nil {
		if card.Transport == "—" {
			card.Transport = pickTransport(runtime.Actual.TransportStates)
		}
		card.Handshake = handshakeSummary(runtime.Actual.Tunnels)
	}
	age := now.UTC().Sub(st.LastHeartbeat.UTC())
	switch {
	case age > 45*time.Second || !st.Healthy:
		card.Status = "unhealthy"
		card.StatusClass = "status-unhealthy"
	case isDegraded(card.Transport):
		card.Status = "degraded"
		card.StatusClass = "status-degraded"
	default:
		card.Status = "healthy"
		card.StatusClass = "status-healthy"
	}
	return card
}

func pickTransport(states []model.TransportState) string {
	if len(states) == 0 {
		return "—"
	}
	rank := map[model.TransportMode]int{
		model.TransportWGPrimary:          1,
		model.TransportSSHPrimary:         2,
		model.TransportFailbackInProgress: 3,
		model.TransportDegraded:           4,
	}
	best := states[0].State
	for _, st := range states[1:] {
		if rank[st.State] > rank[best] {
			best = st.State
		}
	}
	if best == "" {
		return "—"
	}
	return string(best)
}

func isDegraded(transport string) bool {
	switch model.TransportMode(transport) {
	case model.TransportDegraded, model.TransportFailbackInProgress, model.TransportSSHPrimary:
		return true
	default:
		return false
	}
}

func handshakeSummary(tunnels []model.TunnelActual) string {
	var max int64 = -1
	for _, t := range tunnels {
		if t.HandshakeAgeSec > max {
			max = t.HandshakeAgeSec
		}
	}
	if max < 0 {
		return "—"
	}
	return fmtHandshake(max)
}

func failbackBackends(ds *model.DesiredState, runtime *model.NodeActualState) []model.Backend {
	if ds == nil {
		return nil
	}
	want := map[model.ID]bool{}
	if runtime != nil && runtime.Actual != nil {
		for _, ts := range runtime.Actual.TransportStates {
			if ts.State == model.TransportSSHPrimary || ts.State == model.TransportDegraded || ts.State == model.TransportFailbackInProgress {
				want[ts.BackendID] = true
			}
		}
	}
	if runtime != nil && runtime.Status != nil {
		for _, ts := range runtime.Status.TransportStates {
			if ts.State == model.TransportSSHPrimary || ts.State == model.TransportDegraded || ts.State == model.TransportFailbackInProgress {
				want[ts.BackendID] = true
			}
		}
	}
	var out []model.Backend
	for _, b := range ds.Backends {
		if want[b.ID] {
			out = append(out, b)
		}
	}
	return out
}

type tunnelRow struct {
	ID        string
	Type      string
	Node      string
	Backend   string
	Interface string
	Handshake string
	Rx        string
	Tx        string
	Healthy   bool
	KeyPath   string
}

func buildTunnelRow(t model.Tunnel, cat Catalog, actual *model.TunnelActual) tunnelRow {
	row := tunnelRow{
		ID:        string(t.ID),
		Type:      string(t.Type),
		Node:      cat.NodeName(t.NodeID),
		Backend:   cat.BackendName(t.BackendID),
		Interface: t.InterfaceName,
		Handshake: "—",
		Rx:        "—",
		Tx:        "—",
		KeyPath:   t.PrivateKeyPath,
	}
	if actual != nil {
		row.Handshake = fmtHandshake(actual.HandshakeAgeSec)
		row.Rx = fmtBytes(actual.RxBytes)
		row.Tx = fmtBytes(actual.TxBytes)
		row.Healthy = actual.InterfacePresent && (t.Type != model.TunnelWireGuard || actual.HandshakeAgeSec < 180)
	}
	return row
}

func findTunnelActual(runtime *model.NodeActualState, t model.Tunnel) *model.TunnelActual {
	if runtime == nil || runtime.Actual == nil {
		return nil
	}
	for i := range runtime.Actual.Tunnels {
		a := &runtime.Actual.Tunnels[i]
		if a.TunnelID == t.ID || a.InterfaceName == t.InterfaceName {
			return a
		}
	}
	return nil
}

type sniRow struct {
	ID       string
	Node     string
	Listen   string
	Match    string
	Backend  string
	Priority int
	Default  bool
}

func flattenSni(routes []model.SniRoute, cat Catalog) []sniRow {
	var out []sniRow
	for _, r := range routes {
		for i, m := range r.Matches {
			match := m.Match
			if m.Default || match == "" {
				match = "default"
			}
			backend := m.Backend
			if backend == "" {
				backend = cat.BackendName(m.BackendID)
			}
			out = append(out, sniRow{
				ID:       string(r.ID),
				Node:     cat.NodeName(r.NodeID),
				Listen:   r.Listen,
				Match:    match,
				Backend:  backend,
				Priority: i,
				Default:  m.Default,
			})
		}
	}
	return out
}

func sniMatchesText(r model.SniRoute, cat Catalog) (defaultBackend, extra string) {
	var lines []string
	for _, m := range r.Matches {
		if m.Default {
			defaultBackend = m.Backend
			if defaultBackend == "" {
				defaultBackend = string(m.BackendID)
			}
			if name := cat.BackendName(m.BackendID); name != string(m.BackendID) {
				defaultBackend = name
			}
			continue
		}
		b := m.Backend
		if b == "" {
			b = cat.BackendName(m.BackendID)
		}
		lines = append(lines, m.Match+" "+b)
	}
	return defaultBackend, strings.Join(lines, "\n")
}

func parseSniMatches(defaultBackend, extra string) []model.SniMatch {
	matches := []model.SniMatch{{Default: true, Backend: strings.TrimSpace(defaultBackend)}}
	for _, line := range strings.Split(extra, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		matches = append(matches, model.SniMatch{Match: parts[0], Backend: parts[1]})
	}
	return matches
}

func parseAllowedIPs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func resultLabel(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}
