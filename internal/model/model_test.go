package model_test

import (
	"testing"

	"transitforge/internal/model"
)

func TestNodeValidate(t *testing.T) {
	n := model.Node{Name: "ru-edge-1", PublicIP: "203.0.113.10"}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := model.Node{Name: "Invalid Name"}
	if err := bad.Validate(); err == nil {
		t.Fatal("uppercase name should fail")
	}
}

func TestTunnelValidate(t *testing.T) {
	wg := model.Tunnel{
		NodeID: "n", BackendID: "b", Type: model.TunnelWireGuard,
		InterfaceName: "wg-a", LocalOverlayIP: "10.200.1.1", RemoteOverlayIP: "10.200.1.2",
		ListenPort: 51820, AllowedIPs: []string{"10.200.1.2/32"},
	}
	if err := wg.Validate(); err != nil {
		t.Fatal(err)
	}
	ssh := model.Tunnel{
		NodeID: "n", BackendID: "b", Type: model.TunnelSSHTUN,
		InterfaceName: "tun-b", LocalOverlayIP: "10.200.2.1", RemoteOverlayIP: "10.200.2.2",
		ServiceName: "transitforge-ssh.service",
	}
	if err := ssh.Validate(); err != nil {
		t.Fatal(err)
	}
	missing := wg
	missing.ListenPort = 0
	if err := missing.Validate(); err == nil {
		t.Fatal("wg without listen_port")
	}
}

func TestParseRole(t *testing.T) {
	r, err := model.ParseRole("agent")
	if err != nil || r != model.RoleAgent {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := model.ParseRole("nope"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := model.ParseRole("administrator"); err == nil {
		t.Fatal("administrator is not an API token role")
	}
	hr, err := model.ParseHumanRole("administrator")
	if err != nil || hr != model.RoleAdministrator {
		t.Fatalf("%v %v", hr, err)
	}
}

func TestSniRouteValidate(t *testing.T) {
	r := model.SniRoute{
		NodeID: "n",
		Listen: ":443",
		Matches: []model.SniMatch{
			{Match: "example.com", Backend: "backend-a"},
			{Default: true, Backend: "backend-b"},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Matches = []model.SniMatch{{Match: "example.com", Backend: "backend-a"}}
	if err := r.Validate(); err == nil {
		t.Fatal("missing default")
	}
}

func TestFailoverDefaults(t *testing.T) {
	p := model.DefaultFailoverPolicy()
	p.NodeID = "n"
	p.BackendID = "b"
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if !p.AutomaticFailback || p.AutomaticFailforward || p.FailureThreshold != 3 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}
