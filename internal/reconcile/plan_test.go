package reconcile_test

import (
	"strings"
	"testing"

	"transitforge/internal/model"
	"transitforge/internal/reconcile"
)

func fixtureDesired() model.DesiredState {
	nid := model.ID("node1")
	bid := model.ID("be1")
	mid := model.ID("map1")
	tid := model.ID("tun1")
	return model.DesiredState{
		Node:     model.Node{ID: nid, Name: "ru-edge-1"},
		Backends: []model.Backend{{ID: bid, Name: "backend-a", NodeID: nid, Address: "10.200.1.2"}},
		Tunnels: []model.Tunnel{{
			ID: tid, NodeID: nid, BackendID: bid, Type: model.TunnelWireGuard,
			InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
			ListenPort: 51820,
		}},
		Mappings: []model.PortMapping{{
			ID: mid, NodeID: nid, BackendID: bid, Protocol: model.ProtoUDP, PublicPort: 51821, BackendPort: 51820,
		}},
	}
}

func TestDiffEmptyActualAdds(t *testing.T) {
	ds := fixtureDesired()
	plan := reconcile.Diff(ds, model.ActualState{})
	s := plan.String()
	if plan.Empty() {
		t.Fatal("expected adds")
	}
	if !strings.Contains(s, "ADD: tunnel") || !strings.Contains(s, "ADD: firewall") {
		t.Fatalf("plan:\n%s", s)
	}
}

func TestDiffIdempotentWhenActualMatches(t *testing.T) {
	ds := fixtureDesired()
	m := ds.Mappings[0]
	tun := ds.Tunnels[0]
	actual := model.ActualState{
		Tunnels: []model.TunnelActual{{
			TunnelID: tun.ID, Type: tun.Type, InterfaceName: tun.InterfaceName,
			InterfacePresent: true, LocalOverlayIP: tun.LocalOverlayIP, ListenPort: tun.ListenPort,
		}},
		FirewallRules: []model.FirewallRule{{
			Chain: "TRANSITFORGE_DNAT", Comment: reconcile.MappingComment(m.ID), Managed: true,
			Spec: "UDP dport 51821 -> 10.200.1.2:51820 comment " + reconcile.MappingComment(m.ID),
		}},
	}
	plan := reconcile.Diff(ds, actual)
	if !plan.Empty() {
		t.Fatalf("expected NO CHANGES, got:\n%s", plan.String())
	}
	if plan.String() != "NO CHANGES\n" {
		t.Fatalf("got %q", plan.String())
	}
}

func TestDiffConflictSurfaced(t *testing.T) {
	ds := fixtureDesired()
	actual := model.ActualState{
		Conflicts: []model.Conflict{{Code: "PORT_OWNED", Target: "udp/51821", Message: "unrelated process owns port"}},
	}
	plan := reconcile.Diff(ds, actual)
	if !plan.HasConflicts() {
		t.Fatal("expected conflicts")
	}
	if !strings.Contains(plan.String(), "CONFLICT:") {
		t.Fatalf("plan:\n%s", plan.String())
	}
}
