package firewall_test

import (
	"context"
	"strings"
	"testing"

	"proxyctl/internal/firewall"
	"proxyctl/internal/model"
	"proxyctl/internal/testhost"
)

func TestFirewallDiscoverPlanApply(t *testing.T) {
	h := testhost.New()
	m := &firewall.IptablesNftManager{Runner: h, BackupDir: t.TempDir()}
	ctx := context.Background()
	mp := model.PortMapping{ID: "map1", NodeID: "n", BackendID: "b", Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 8443}
	backends := []model.Backend{{ID: "b", Address: "10.200.1.2"}}
	rules, conflicts, err := m.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("%v", conflicts)
	}
	plan := m.Plan([]model.PortMapping{mp}, backends, rules)
	if plan.Empty() {
		t.Fatal("expected ADD")
	}
	if err := m.Apply(ctx, plan, []model.PortMapping{mp}, backends); err != nil {
		t.Fatal(err)
	}
	rules2, _, err := m.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan2 := m.Plan([]model.PortMapping{mp}, backends, rules2)
	if !plan2.Empty() {
		t.Fatalf("expected empty second plan:\n%s", plan2)
	}
	if !strings.Contains(strings.Join(h.NAT["PROXYCTL_DNAT"], "\n"), "proxyctl:mapping:map1") {
		t.Fatalf("dnat=%v", h.NAT["PROXYCTL_DNAT"])
	}
}

func TestFirewallRejectsBadIP(t *testing.T) {
	h := testhost.New()
	m := &firewall.IptablesNftManager{Runner: h, BackupDir: t.TempDir()}
	mp := model.PortMapping{ID: "map1", NodeID: "n", BackendID: "b", Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443}
	plan := m.Plan([]model.PortMapping{mp}, []model.Backend{{ID: "b", Address: "not-an-ip"}}, nil)
	err := m.Apply(context.Background(), plan, []model.PortMapping{mp}, []model.Backend{{ID: "b", Address: "not-an-ip"}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
