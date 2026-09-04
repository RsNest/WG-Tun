package webui

import (
	"sort"
	"strconv"
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
	MappingLabel  string
	AgentStatus   string
	AgentReported bool
	Degraded      bool
	ReasonKey     string
}

func buildNodeCard(now time.Time, n model.Node, mappings []model.PortMapping, runtime *model.NodeActualState, locale string) nodeCard {
	card := nodeCard{
		ID:            string(n.ID),
		Name:          n.Name,
		PublicIP:      n.PublicIP,
		Status:        "unknown",
		StatusClass:   "status-unknown",
		Transport:     "—",
		Handshake:     "—",
		LastReconcile: "—",
		Heartbeat:     "—",
		AgentStatus:   "unknown",
		MappingLabel:  "—",
		ReasonKey:     "status.reason.agent_not_reported",
	}
	nMaps := 0
	for _, m := range mappings {
		if m.NodeID == n.ID && m.Enabled {
			nMaps++
		}
	}
	card.MappingCount = nMaps
	card.MappingLabel = strconv.Itoa(nMaps)
	if runtime == nil || runtime.Status == nil || runtime.Status.LastHeartbeat.IsZero() {
		return card
	}
	card.AgentReported = true
	st := runtime.Status
	card.Heartbeat = relTime(now, st.LastHeartbeat, locale)
	if st.LastReconcile.IsZero() {
		card.LastReconcile = "—"
	} else {
		card.LastReconcile = relTime(now, st.LastReconcile, locale)
	}
	card.Transport = pickTransport(st.TransportStates)
	if runtime.Actual != nil {
		if card.Transport == "—" {
			card.Transport = pickTransport(runtime.Actual.TransportStates)
		}
		card.Handshake = handshakeSummary(runtime.Actual.Tunnels)
	}
	age := now.UTC().Sub(st.LastHeartbeat.UTC())
	switch {
	case age > 45*time.Second:
		card.Status = "unhealthy"
		card.StatusClass = "status-unhealthy"
		card.AgentStatus = "unhealthy"
		card.ReasonKey = "status.reason.heartbeat_stale"
	case !st.Healthy:
		card.Status = "unhealthy"
		card.StatusClass = "status-unhealthy"
		card.AgentStatus = "unhealthy"
		card.ReasonKey = "status.reason.agent_unhealthy"
	case isDegraded(card.Transport):
		card.Status = "degraded"
		card.StatusClass = "status-degraded"
		card.AgentStatus = "healthy"
		card.Degraded = true
		card.ReasonKey = "status.reason.transport_degraded"
	case staleHandshake(card.Handshake):
		card.Status = "warning"
		card.StatusClass = "status-warning"
		card.AgentStatus = "healthy"
		card.ReasonKey = "status.reason.handshake_stale"
	default:
		card.Status = "healthy"
		card.StatusClass = "status-healthy"
		card.AgentStatus = "healthy"
		card.ReasonKey = "status.reason.agent_healthy"
	}
	return card
}

func staleHandshake(s string) bool {
	return strings.HasSuffix(s, "h")
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
	ID          string
	Type        string
	Node        string
	Backend     string
	Interface   string
	Endpoint    string
	Handshake   string
	Rx          string
	Tx          string
	Priority    int
	Status      string
	StatusClass string
	Healthy     bool
	KeyPath     string
	ReasonKey   string
}

func buildTunnelRow(t model.Tunnel, cat Catalog, actual *model.TunnelActual) tunnelRow {
	row := tunnelRow{
		ID:          string(t.ID),
		Type:        string(t.Type),
		Node:        cat.NodeName(t.NodeID),
		Backend:     cat.BackendName(t.BackendID),
		Interface:   t.InterfaceName,
		Endpoint:    t.Endpoint,
		Handshake:   "—",
		Rx:          "—",
		Tx:          "—",
		Priority:    t.Priority,
		Status:      "unknown",
		StatusClass: "status-unknown",
		KeyPath:     t.PrivateKeyPath,
		ReasonKey:   "status.reason.tunnel_unknown",
	}
	if actual == nil {
		return row
	}
	row.Handshake = fmtHandshake(actual.HandshakeAgeSec)
	if actual.RxBytes > 0 {
		row.Rx = fmtBytes(actual.RxBytes)
	}
	if actual.TxBytes > 0 {
		row.Tx = fmtBytes(actual.TxBytes)
	}
	row.Healthy = actual.InterfacePresent && (t.Type != model.TunnelWireGuard || actual.HandshakeAgeSec < 180)
	if !actual.InterfacePresent {
		row.Status = "unhealthy"
		row.StatusClass = "status-unhealthy"
		row.ReasonKey = "status.reason.interface_missing"
	} else if t.Type == model.TunnelWireGuard && actual.HandshakeAgeSec >= 180 {
		row.Status = "warning"
		row.StatusClass = "status-warning"
		row.ReasonKey = "status.reason.handshake_warning"
	} else {
		row.Status = "healthy"
		row.StatusClass = "status-healthy"
		row.ReasonKey = "status.reason.tunnel_healthy"
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		if out[i].Listen != out[j].Listen {
			return out[i].Listen < out[j].Listen
		}
		return out[i].Priority < out[j].Priority
	})
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

func managementAddr(n model.Node) string {
	if n.Labels == nil {
		return ""
	}
	for _, k := range []string{"mgmt", "management", "mgmt_ip", "management_ip"} {
		if v := strings.TrimSpace(n.Labels[k]); v != "" {
			return v
		}
	}
	return ""
}

func formatLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ", ")
}

type eventRow struct {
	Key     string
	When    string
	Actor   string
	Action  string
	Target  string
	Detail  string
	Success bool
}

func buildEventRows(events []model.AuditEvent) []eventRow {
	out := make([]eventRow, 0, len(events))
	for _, e := range events {
		target := strings.TrimSpace(e.Resource + " " + e.ResourceID)
		out = append(out, eventRow{
			Key:     eventKey(e),
			When:    e.Timestamp.UTC().Format("2006-01-02 15:04:05"),
			Actor:   e.Actor,
			Action:  e.Action,
			Target:  target,
			Detail:  safeEventDetail(e.Detail),
			Success: e.Success,
		})
	}
	return out
}

func eventKey(e model.AuditEvent) string {
	return strconv.FormatInt(e.Timestamp.UnixNano(), 10) + "-" + e.Action + "-" + e.ResourceID
}

func filterEventRows(rows []eventRow, q, actor, result string) []eventRow {
	q = strings.ToLower(strings.TrimSpace(q))
	actor = strings.ToLower(strings.TrimSpace(actor))
	result = strings.ToLower(strings.TrimSpace(result))
	if q == "" && actor == "" && result == "" {
		return rows
	}
	out := make([]eventRow, 0, len(rows))
	for _, row := range rows {
		if actor != "" && !strings.Contains(strings.ToLower(row.Actor), actor) {
			continue
		}
		if result == "ok" && !row.Success {
			continue
		}
		if result == "error" && row.Success {
			continue
		}
		if q != "" {
			blob := strings.ToLower(row.When + " " + row.Actor + " " + row.Action + " " + row.Target + " " + row.Detail)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		out = append(out, row)
	}
	return out
}

func emptyForm() map[string]string {
	return map[string]string{}
}

func resultLabel(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}
