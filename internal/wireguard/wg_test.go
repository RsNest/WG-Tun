package wireguard_test

import (
	"context"
	"strings"
	"testing"

	"proxyctl/internal/model"
	"proxyctl/internal/testhost"
	"proxyctl/internal/wireguard"
)

func TestWGApplyDiscover(t *testing.T) {
	h := testhost.New()
	m := &wireguard.WGManager{Runner: h}
	tunn := model.Tunnel{
		ID: "t1", NodeID: "n", BackendID: "b", Type: model.TunnelWireGuard,
		InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
		ListenPort: 51820, Endpoint: "198.51.100.20:51820", AllowedIPs: []string{"10.200.1.2/32"},
		PrivateKeyPath: "/etc/proxyctl/keys/wg-a.key", PublicKey: "pubKEY0123456789abcdefghijklmnopqrstuv",
	}
	if err := m.Apply(context.Background(), tunn); err != nil {
		t.Fatal(err)
	}
	acts, conflicts, err := m.Discover(context.Background(), []model.Tunnel{tunn})
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("%v", conflicts)
	}
	if len(acts) != 1 || !acts[0].InterfacePresent {
		t.Fatalf("%+v", acts)
	}
	if acts[0].LocalOverlayIP != "10.200.1.1" || acts[0].ListenPort != 51820 {
		t.Fatalf("%+v", acts[0])
	}
	for _, c := range h.Calls {
		j := strings.Join(c, " ")
		if strings.Contains(strings.ToLower(j), "private-key") && !strings.Contains(j, "/etc/proxyctl/keys/wg-a.key") {
			t.Fatalf("private key material in args: %s", j)
		}
		if strings.Contains(j, "BEGIN") {
			t.Fatal("key block in command")
		}
	}
}

func TestWGMissingPrivateKeyPath(t *testing.T) {
	m := &wireguard.WGManager{Runner: testhost.New()}
	tunn := model.Tunnel{
		ID: "t1", NodeID: "n", BackendID: "b", Type: model.TunnelWireGuard,
		InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
		ListenPort: 51820,
	}
	if err := m.Apply(context.Background(), tunn); err == nil {
		t.Fatal("expected conflict without key path")
	}
}
