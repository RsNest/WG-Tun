package failover_test

import (
	"testing"
	"time"

	"transitforge/internal/failover"
	"transitforge/internal/health"
	"transitforge/internal/model"
)

func pol() model.FailoverPolicy {
	p := model.DefaultFailoverPolicy()
	p.NodeID = "n"
	p.BackendID = "b"
	return p
}

func TestThresholdThenFailback(t *testing.T) {
	st := model.TransportState{NodeID: "n", BackendID: "b", State: model.TransportWGPrimary}
	primDown := health.Snapshot{InterfacePresent: false}
	fbUp := health.Snapshot{UnitActive: true, InterfacePresent: true, OverlayReachable: true}
	now := time.Now()
	for i := 0; i < 3; i++ {
		d := failover.Step(failover.Input{Current: st, Policy: pol(), Primary: primDown, Fallback: fbUp, Now: now})
		st = d.Next
		if i < 2 && d.CutoverToSSH {
			t.Fatalf("cutover too early at %d", i)
		}
		if i == 2 && !d.CutoverToSSH {
			t.Fatalf("expected cutover: %+v", d)
		}
	}
	if st.State != model.TransportFailbackInProgress {
		t.Fatalf("state %s", st.State)
	}
}

func TestNoHealthyTransport(t *testing.T) {
	st := model.TransportState{State: model.TransportWGPrimary, ConsecutiveFailures: 3}
	d := failover.Step(failover.Input{
		Current: st, Policy: pol(),
		Primary:  health.Snapshot{InterfacePresent: false},
		Fallback: health.Snapshot{},
		Now:      time.Now(),
	})
	if !d.Critical || d.Next.State != model.TransportDegraded {
		t.Fatalf("%+v", d)
	}
	if d.CutoverToSSH {
		t.Fatal("must not cutover without healthy fallback")
	}
}

func TestNoAutomaticFailForward(t *testing.T) {
	st := model.TransportState{State: model.TransportSSHPrimary}
	p := pol()
	p.AutomaticFailforward = true
	d := failover.Step(failover.Input{
		Current: st, Policy: p,
		Primary:  health.Snapshot{InterfacePresent: true, OverlayReachable: true},
		Fallback: health.Snapshot{UnitActive: true, InterfacePresent: true, OverlayReachable: true},
		Now:      time.Now(),
	})
	if d.CutoverToWG || d.Next.State != model.TransportSSHPrimary {
		t.Fatalf("must not auto fail-forward: %+v", d)
	}
	d = failover.Step(failover.Input{
		Current: st, Policy: p, FailForward: true,
		Primary:  health.Snapshot{InterfacePresent: true, OverlayReachable: true},
		Fallback: health.Snapshot{UnitActive: true, InterfacePresent: true, OverlayReachable: true},
		Now:      time.Now(),
	})
	if !d.CutoverToWG || d.Next.State != model.TransportWGPrimary {
		t.Fatalf("operator fail-forward: %+v", d)
	}
}

func TestRefuseFailForwardIfPrimaryUnhealthy(t *testing.T) {
	st := model.TransportState{State: model.TransportSSHPrimary}
	d := failover.Step(failover.Input{
		Current: st, Policy: pol(), FailForward: true,
		Primary:  health.Snapshot{InterfacePresent: false},
		Fallback: health.Snapshot{UnitActive: true, InterfacePresent: true, OverlayReachable: true},
		Now:      time.Now(),
	})
	if d.CutoverToWG {
		t.Fatal("must not fail-forward to unhealthy WG")
	}
}
