package metrics_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"transitforge/internal/metrics"
	"transitforge/internal/model"
)

func TestMetricsExpose(t *testing.T) {
	m := metrics.New()
	m.ObserveReconcile("ru-edge-1", true, model.ActualState{
		Tunnels: []model.TunnelActual{{
			TunnelID: "t1", Type: model.TunnelWireGuard, InterfacePresent: true, HandshakeAgeSec: 12, RxBytes: 10, TxBytes: 20,
		}},
		TransportStates: []model.TransportState{{BackendID: "b1", State: model.TransportWGPrimary}},
	})
	ts := httptest.NewServer(m.Handler())
	t.Cleanup(ts.Close)
	resp, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	s := string(b)
	for _, n := range []string{"transitforge_agent_up", "transitforge_reconcile_success", "transitforge_wireguard_handshake_age_seconds", "transitforge_transport_state"} {
		if !strings.Contains(s, n) {
			t.Fatalf("missing %s in %s", n, s)
		}
	}
}
