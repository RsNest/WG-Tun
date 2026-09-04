package metrics

import (
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"

	"proxyctl/internal/model"
)

type Metrics struct {
	reg *prometheus.Registry

	AgentUp           prometheus.Gauge
	ReconcileSuccess  prometheus.Gauge
	ReconcileErrors   prometheus.Counter
	TunnelUp          *prometheus.GaugeVec
	HandshakeAge      *prometheus.GaugeVec
	WGRx              *prometheus.GaugeVec
	WGTx              *prometheus.GaugeVec
	ProbeSuccess      *prometheus.GaugeVec
	TransportState    *prometheus.GaugeVec
	FailbackTotal     *prometheus.CounterVec
	FailbackDuration  prometheus.Histogram
	ConfigApply       prometheus.Counter
	ConfigApplyErrors prometheus.Counter
	LastReconcile     prometheus.Gauge
}

func New() *Metrics {
	r := prometheus.NewRegistry()
	m := &Metrics{reg: r}
	m.AgentUp = prometheus.NewGauge(prometheus.GaugeOpts{Name: "proxyctl_agent_up", Help: "1 if the agent process is running"})
	m.ReconcileSuccess = prometheus.NewGauge(prometheus.GaugeOpts{Name: "proxyctl_reconcile_success", Help: "1 if the last reconcile succeeded"})
	m.ReconcileErrors = prometheus.NewCounter(prometheus.CounterOpts{Name: "proxyctl_reconcile_errors_total", Help: "Reconcile errors"})
	m.TunnelUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_tunnel_up", Help: "Tunnel interface up"}, []string{"node", "backend", "transport"})
	m.HandshakeAge = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_wireguard_handshake_age_seconds", Help: "WireGuard handshake age"}, []string{"node", "backend"})
	m.WGRx = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_wireguard_rx_bytes_total", Help: "WireGuard RX bytes"}, []string{"node", "backend"})
	m.WGTx = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_wireguard_tx_bytes_total", Help: "WireGuard TX bytes"}, []string{"node", "backend"})
	m.ProbeSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_backend_probe_success", Help: "Backend probe success"}, []string{"node", "backend", "port"})
	m.TransportState = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "proxyctl_transport_state", Help: "Transport state (1=active)"}, []string{"node", "backend", "state"})
	m.FailbackTotal = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "proxyctl_failback_total", Help: "Failback events"}, []string{"node", "backend"})
	m.FailbackDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "proxyctl_failback_duration_seconds", Help: "Failback duration", Buckets: []float64{0.5, 1, 2, 5, 10, 30}})
	m.ConfigApply = prometheus.NewCounter(prometheus.CounterOpts{Name: "proxyctl_config_apply_total", Help: "Config applies"})
	m.ConfigApplyErrors = prometheus.NewCounter(prometheus.CounterOpts{Name: "proxyctl_config_apply_errors_total", Help: "Config apply errors"})
	m.LastReconcile = prometheus.NewGauge(prometheus.GaugeOpts{Name: "proxyctl_last_successful_reconcile_timestamp", Help: "Unix timestamp of last successful reconcile"})
	r.MustRegister(m.AgentUp, m.ReconcileSuccess, m.ReconcileErrors, m.TunnelUp, m.HandshakeAge, m.WGRx, m.WGTx, m.ProbeSuccess, m.TransportState, m.FailbackTotal, m.FailbackDuration, m.ConfigApply, m.ConfigApplyErrors, m.LastReconcile)
	m.AgentUp.Set(1)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveReconcile(node string, success bool, actual model.ActualState) {
	if success {
		m.ReconcileSuccess.Set(1)
		m.LastReconcile.SetToCurrentTime()
		m.ConfigApply.Inc()
	} else {
		m.ReconcileSuccess.Set(0)
		m.ReconcileErrors.Inc()
		m.ConfigApplyErrors.Inc()
	}
	backendOf := map[string]string{}
	for _, t := range actual.Tunnels {
		be := string(t.TunnelID)
		transport := strings.ToLower(string(t.Type))
		up := 0.0
		if t.InterfacePresent {
			up = 1
		}
		m.TunnelUp.WithLabelValues(node, be, transport).Set(up)
		if t.Type == model.TunnelWireGuard {
			m.HandshakeAge.WithLabelValues(node, be).Set(float64(t.HandshakeAgeSec))
			m.WGRx.WithLabelValues(node, be).Set(float64(t.RxBytes))
			m.WGTx.WithLabelValues(node, be).Set(float64(t.TxBytes))
		}
		backendOf[string(t.TunnelID)] = be
	}
	for _, ts := range actual.TransportStates {
		be := string(ts.BackendID)
		for _, st := range []model.TransportMode{model.TransportWGPrimary, model.TransportFailbackInProgress, model.TransportSSHPrimary, model.TransportDegraded} {
			v := 0.0
			if ts.State == st {
				v = 1
			}
			m.TransportState.WithLabelValues(node, be, string(st)).Set(v)
		}
	}
}

func (m *Metrics) ObserveProbe(node, backend string, port int, ok bool) {
	v := 0.0
	if ok {
		v = 1
	}
	m.ProbeSuccess.WithLabelValues(node, backend, strconv.Itoa(port)).Set(v)
}

func (m *Metrics) ObserveFailback(node, backend string, seconds float64) {
	m.FailbackTotal.WithLabelValues(node, backend).Inc()
	m.FailbackDuration.Observe(seconds)
}
