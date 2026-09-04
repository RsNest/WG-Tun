package health_test

import (
	"testing"

	"proxyctl/internal/health"
)

func TestPrimaryFailedRules(t *testing.T) {
	if !health.PrimaryFailed(health.Snapshot{InterfacePresent: false}, 2) {
		t.Fatal("missing iface")
	}
	if health.PrimaryFailed(health.Snapshot{InterfacePresent: true, OverlayReachable: true}, 2) {
		t.Fatal("healthy")
	}
	bad := health.Snapshot{InterfacePresent: true, OverlayReachable: false, HandshakeAgeSec: 200, ProbesTotal: 2, ProbesFailed: 2}
	if !health.PrimaryFailed(bad, 2) {
		t.Fatal("stale+probes")
	}
}

func TestFallbackHealthy(t *testing.T) {
	if health.FallbackHealthy(health.Snapshot{InterfacePresent: true, OverlayReachable: true}) {
		t.Fatal("unit must be active")
	}
	if !health.FallbackHealthy(health.Snapshot{UnitActive: true, InterfacePresent: true, OverlayReachable: true}) {
		t.Fatal("should be healthy")
	}
}
