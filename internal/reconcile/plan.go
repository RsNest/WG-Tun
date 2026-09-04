package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"transitforge/internal/model"
)

type ActionKind string

const (
	KindAdd    ActionKind = "ADD"
	KindChange ActionKind = "CHANGE"
	KindDelete ActionKind = "DELETE"
)

type Action struct {
	Kind   ActionKind `json:"kind"`
	Target string     `json:"target"`
	ID     string     `json:"id,omitempty"`
	Before string     `json:"before,omitempty"`
	After  string     `json:"after,omitempty"`
	Reason string     `json:"reason,omitempty"`
}

type Plan struct {
	Actions   []Action         `json:"actions"`
	Conflicts []model.Conflict `json:"conflicts,omitempty"`
}

func (p Plan) Empty() bool {
	return len(p.Actions) == 0
}

func (p Plan) HasConflicts() bool {
	return len(p.Conflicts) > 0
}

func (p Plan) String() string {
	if p.HasConflicts() {
		var b strings.Builder
		b.WriteString("CONFLICT:\n")
		for _, c := range p.Conflicts {
			fmt.Fprintf(&b, "CONFLICT: %s %s: %s\n", c.Code, c.Target, c.Message)
		}
		return b.String()
	}
	if p.Empty() {
		return "NO CHANGES\n"
	}
	var b strings.Builder
	for _, a := range p.Actions {
		switch a.Kind {
		case KindAdd:
			fmt.Fprintf(&b, "ADD: %s %s\n", a.Target, a.After)
		case KindChange:
			fmt.Fprintf(&b, "CHANGE: %s %s -> %s\n", a.Target, a.Before, a.After)
		case KindDelete:
			fmt.Fprintf(&b, "DELETE: %s %s\n", a.Target, a.Before)
		}
		if a.Reason != "" {
			fmt.Fprintf(&b, "  reason: %s\n", a.Reason)
		}
	}
	return b.String()
}

func Diff(desired model.DesiredState, actual model.ActualState) Plan {
	p := Plan{}
	p.Conflicts = append(p.Conflicts, actual.Conflicts...)

	desiredTunnels := map[string]model.Tunnel{}
	for _, t := range desired.Tunnels {
		desiredTunnels[t.InterfaceName] = t
	}
	actualTunnels := map[string]model.TunnelActual{}
	for _, t := range actual.Tunnels {
		actualTunnels[t.InterfaceName] = t
	}

	var ifaces []string
	seen := map[string]bool{}
	for name := range desiredTunnels {
		ifaces = append(ifaces, name)
		seen[name] = true
	}
	for name := range actualTunnels {
		if !seen[name] {
			ifaces = append(ifaces, name)
		}
	}
	sort.Strings(ifaces)
	for _, name := range ifaces {
		d, hasD := desiredTunnels[name]
		a, hasA := actualTunnels[name]
		switch {
		case hasD && !hasA:
			p.Actions = append(p.Actions, Action{
				Kind:   KindAdd,
				Target: "tunnel",
				ID:     string(d.ID),
				After:  tunnelDesiredLine(d),
			})
		case !hasD && hasA:
			p.Actions = append(p.Actions, Action{
				Kind:   KindDelete,
				Target: "tunnel",
				Before: tunnelActualLine(a),
			})
		case hasD && hasA:
			if tunnelChanged(d, a) {
				p.Actions = append(p.Actions, Action{
					Kind:   KindChange,
					Target: "tunnel",
					ID:     string(d.ID),
					Before: tunnelActualLine(a),
					After:  tunnelDesiredLine(d),
				})
			}
		}
	}

	backendByID := map[model.ID]model.Backend{}
	for _, b := range desired.Backends {
		backendByID[b.ID] = b
	}

	managedComments := map[string]model.FirewallRule{}
	for _, r := range actual.FirewallRules {
		if r.Managed && r.Comment != "" {
			managedComments[r.Comment] = r
		}
	}
	wantedComments := map[string]model.PortMapping{}
	for _, m := range desired.Mappings {
		cmt := MappingComment(m.ID)
		wantedComments[cmt] = m
	}
	var comments []string
	seenC := map[string]bool{}
	for c := range wantedComments {
		comments = append(comments, c)
		seenC[c] = true
	}
	for c := range managedComments {
		if !seenC[c] {
			comments = append(comments, c)
		}
	}
	sort.Strings(comments)
	for _, c := range comments {
		m, hasW := wantedComments[c]
		r, hasA := managedComments[c]
		want := mappingRuleSpec(m, backendByID[m.BackendID])
		switch {
		case hasW && !hasA:
			p.Actions = append(p.Actions, Action{
				Kind:   KindAdd,
				Target: "firewall",
				ID:     string(m.ID),
				After:  want,
			})
		case !hasW && hasA:
			p.Actions = append(p.Actions, Action{
				Kind:   KindDelete,
				Target: "firewall",
				Before: r.Spec,
			})
		case hasW && hasA && r.Spec != want:
			p.Actions = append(p.Actions, Action{
				Kind:   KindChange,
				Target: "firewall",
				ID:     string(m.ID),
				Before: r.Spec,
				After:  want,
			})
		}
	}

	if len(desired.SniRoutes) > 0 {
		wantDigest := SniDigest(desired.SniRoutes)
		if actual.HaproxyDigest == "" {
			for _, r := range desired.SniRoutes {
				p.Actions = append(p.Actions, Action{
					Kind:   KindAdd,
					Target: "haproxy",
					ID:     string(r.ID),
					After:  sniLine(r),
				})
			}
		} else if actual.HaproxyDigest != wantDigest {
			p.Actions = append(p.Actions, Action{
				Kind:   KindChange,
				Target: "haproxy",
				Before: "digest=" + actual.HaproxyDigest,
				After:  "digest=" + wantDigest,
			})
		}
	} else if actual.HaproxyDigest != "" && len(actual.SniRoutes) > 0 {
		p.Actions = append(p.Actions, Action{
			Kind:   KindDelete,
			Target: "haproxy",
			Before: "managed SNI sections",
		})
	}

	return p
}

func MappingComment(id model.ID) string {
	return "transitforge:mapping:" + string(id)
}

func mappingRuleSpec(m model.PortMapping, b model.Backend) string {
	dst := b.Address
	if dst == "" {
		dst = string(m.BackendID)
	}
	return fmt.Sprintf("%s dport %d -> %s:%d comment %s", m.Protocol, m.PublicPort, dst, m.BackendPort, MappingComment(m.ID))
}

func tunnelDesiredLine(t model.Tunnel) string {
	return fmt.Sprintf("%s %s %s->%s if=%s port=%d endpoint=%s", t.Type, t.ID, t.LocalOverlayIP, t.RemoteOverlayIP, t.InterfaceName, t.ListenPort, t.Endpoint)
}

func tunnelActualLine(a model.TunnelActual) string {
	return fmt.Sprintf("%s if=%s present=%t local=%s hs_age=%d", a.Type, a.InterfaceName, a.InterfacePresent, a.LocalOverlayIP, a.HandshakeAgeSec)
}

func tunnelChanged(d model.Tunnel, a model.TunnelActual) bool {
	if !a.InterfacePresent {
		return true
	}
	if d.LocalOverlayIP != "" && a.LocalOverlayIP != "" && d.LocalOverlayIP != a.LocalOverlayIP {
		return true
	}
	if d.ListenPort != 0 && a.ListenPort != 0 && d.ListenPort != a.ListenPort {
		return true
	}
	return false
}

func sniLine(r model.SniRoute) string {
	return fmt.Sprintf("listen %s matches=%d", r.Listen, len(r.Matches))
}

func SniDigest(routes []model.SniRoute) string {
	var parts []string
	for _, r := range routes {
		parts = append(parts, sniLine(r))
		for _, m := range r.Matches {
			parts = append(parts, fmt.Sprintf("%s:%s:%s:%t", m.Match, m.Backend, m.BackendID, m.Default))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}
