package engine_test

import (
	"context"
	"strings"
	"testing"

	"proxyctl/internal/engine"
	"proxyctl/internal/firewall"
	"proxyctl/internal/haproxy"
	"proxyctl/internal/model"
	"proxyctl/internal/testhost"
	"proxyctl/internal/wireguard"
)

func desired() model.DesiredState {
	nid := model.ID("node1")
	bid := model.ID("be1")
	return model.DesiredState{
		Node: model.Node{ID: nid, Name: "ru-edge-1"},
		Backends: []model.Backend{{
			ID: bid, Name: "backend-a", NodeID: nid, Address: "10.200.1.2",
		}},
		Tunnels: []model.Tunnel{{
			ID: "tun1", NodeID: nid, BackendID: bid, Type: model.TunnelWireGuard,
			InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
			ListenPort: 51820, Endpoint: "198.51.100.20:51820", AllowedIPs: []string{"10.200.1.2/32"},
			PrivateKeyPath: "/etc/proxyctl/keys/wg-a.key", PublicKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCD=",
			PersistentKeepalive: 25,
		}},
		Mappings: []model.PortMapping{{
			ID: "map1", NodeID: nid, BackendID: bid, Protocol: model.ProtoUDP, PublicPort: 51821, BackendPort: 51820,
		}, {
			ID: "map2", NodeID: nid, BackendID: bid, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443,
		}},
		SniRoutes: []model.SniRoute{{
			ID: "sni1", NodeID: nid, Listen: ":443",
			Matches: []model.SniMatch{
				{Match: "example.com", Backend: "backend-a", BackendID: bid},
				{Default: true, Backend: "backend-a", BackendID: bid},
			},
		}},
	}
}

func newEngine(h *testhost.Host, dir string) *engine.Engine {
	return &engine.Engine{
		FW: &firewall.IptablesNftManager{Runner: h, BackupDir: dir},
		WG: &wireguard.WGManager{Runner: h},
		HP: &haproxy.Manager{Runner: h, ConfigPath: dir + "/haproxy.cfg", BackupDir: dir + "/hbak"},
	}
}

func TestApplyIdempotent(t *testing.T) {
	h := testhost.New()
	eng := newEngine(h, t.TempDir())
	ds := desired()
	ctx := context.Background()

	res, err := eng.Reconcile(ctx, ds, false)
	if err != nil {
		t.Fatalf("first apply: %v\nplan:\n%s", err, res.Plan)
	}
	if !res.Applied {
		t.Fatalf("expected apply, plan:\n%s", res.Plan)
	}
	if h.MappingRuleCount() != 2 {
		t.Fatalf("expected 2 DNAT rules, got %d", h.MappingRuleCount())
	}

	h.Calls = nil
	res2, err := eng.Reconcile(ctx, ds, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Plan.Empty() {
		t.Fatalf("second plan must be empty, got:\n%s", res2.Plan)
	}
	if res2.Applied {
		t.Fatal("second apply must not apply")
	}
	for _, c := range h.Calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "iptables -A") || strings.Contains(joined, "link add") || strings.Contains(joined, "wg set") {
			t.Fatalf("second reconcile mutated host: %v", c)
		}
	}
}

func TestApplyFailsThenRollback(t *testing.T) {
	h := testhost.New()
	eng := newEngine(h, t.TempDir())
	ds := desired()
	h.FailOn = "link set wg-a up"
	res, err := eng.Reconcile(context.Background(), ds, false)
	if err == nil {
		t.Fatal("expected apply failure")
	}
	if !res.Rolled {
		t.Fatalf("expected rollback, err=%v", err)
	}
	if _, ok := h.Links["wg-a"]; ok {
		t.Fatal("wg-a should have been removed by rollback")
	}
	if h.MappingRuleCount() != 0 {
		t.Fatalf("firewall rules should be rolled back, have %d", h.MappingRuleCount())
	}
}

func TestDryRunDoesNotMutate(t *testing.T) {
	h := testhost.New()
	eng := newEngine(h, t.TempDir())
	res, err := eng.Reconcile(context.Background(), desired(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied {
		t.Fatal("dry-run applied")
	}
	if !strings.Contains(res.Plan.String(), "ADD:") {
		t.Fatalf("plan: %s", res.Plan)
	}
	if len(h.Links) != 0 || h.MappingRuleCount() != 0 {
		t.Fatal("host mutated during dry-run")
	}
}

func TestUnmanagedRuleConflict(t *testing.T) {
	h := testhost.New()
	h.NAT["PROXYCTL_DNAT"] = []string{`-p tcp --dport 22 -m comment --comment ssh-admin -j ACCEPT`}
	eng := newEngine(h, t.TempDir())
	_, err := eng.Reconcile(context.Background(), desired(), false)
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "CONFLICT") {
		t.Fatalf("err=%v", err)
	}
}
