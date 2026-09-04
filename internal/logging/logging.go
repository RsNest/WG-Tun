package logging

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Event names that operators grep in journald.
const (
	EventAgentRegistered    = "agent_registered"
	EventReconcileStarted   = "reconcile_started"
	EventReconcileCompleted = "reconcile_completed"
	EventFirewallChanged    = "firewall_changed"
	EventHaproxyChanged     = "haproxy_changed"
	EventWGHandshakeLost    = "wg_handshake_lost"
	EventTransportDegraded  = "transport_degraded"
	EventFailbackStarted    = "failback_started"
	EventFailbackCompleted  = "failback_completed"
	EventRollbackStarted    = "rollback_started"
	EventRollbackCompleted  = "rollback_completed"
	EventConflict           = "conflict"
	EventAudit              = "audit"
)

type Logger struct {
	mu        sync.Mutex
	w         io.Writer
	component string
	node      string
	minLevel  string
}

func New(component string) *Logger {
	return &Logger{w: os.Stdout, component: component, minLevel: "info"}
}

func (l *Logger) WithNode(node string) *Logger {
	if l == nil {
		return nil
	}
	return &Logger{w: l.w, component: l.component, node: node, minLevel: l.minLevel}
}

type Fields struct {
	Backend    string
	Transport  string
	Event      string
	DurationMS int64
	Error      string
	Extra      map[string]any
}

func (l *Logger) Debug(msg string, f Fields) { l.log("debug", msg, f) }
func (l *Logger) Info(msg string, f Fields)  { l.log("info", msg, f) }
func (l *Logger) Warn(msg string, f Fields)  { l.log("warn", msg, f) }
func (l *Logger) Error(msg string, f Fields) { l.log("error", msg, f) }

func (l *Logger) log(level, msg string, f Fields) {
	if l == nil {
		return
	}
	rec := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     level,
		"component": l.component,
		"msg":       Redact(msg),
	}
	if l.node != "" {
		rec["node"] = l.node
	}
	if f.Backend != "" {
		rec["backend"] = f.Backend
	}
	if f.Transport != "" {
		rec["transport"] = f.Transport
	}
	if f.Event != "" {
		rec["event"] = f.Event
	}
	if f.DurationMS != 0 {
		rec["duration_ms"] = f.DurationMS
	}
	if f.Error != "" {
		rec["error"] = Redact(f.Error)
	}
	for k, v := range f.Extra {
		if isSecretKey(k) {
			rec[k] = "[redacted]"
			continue
		}
		if s, ok := v.(string); ok {
			rec[k] = Redact(s)
			continue
		}
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(b, '\n'))
}

var secretKeys = []string{"token", "password", "secret", "private_key", "authorization", "hmac", "signature"}

func isSecretKey(k string) bool {
	lk := strings.ToLower(k)
	for _, s := range secretKeys {
		if strings.Contains(lk, s) {
			return true
		}
	}
	return false
}

// Redact replaces likely secrets in free-form strings.
func Redact(s string) string {
	if s == "" {
		return s
	}
	out := s
	for _, needle := range []string{"PRIVATE KEY", "wg set", "preshared-key"} {
		if strings.Contains(out, needle) {
			return "[redacted]"
		}
	}
	return out
}
