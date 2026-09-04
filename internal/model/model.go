package model

import (
	"strings"
	"time"
)

// ID is a string UUID assigned by the controller.
type ID string

// Node is an edge host that runs proxyctl-agent.
type Node struct {
	ID        ID                `json:"id"`
	Name      string            `json:"name"`
	PublicIP  string            `json:"public_ip"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Backend is a destination host reachable over an overlay tunnel from a node.
type Backend struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	NodeID    ID        `json:"node_id"`
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TunnelType is the overlay transport used between a node and a backend.
type TunnelType string

const (
	TunnelWireGuard TunnelType = "WIREGUARD"
	TunnelSSHTUN    TunnelType = "SSH_TUN"
)

// Tunnel describes a managed overlay link. Private keys are never stored here;
// only a filesystem path reference (PrivateKeyPath) is persisted.
type Tunnel struct {
	ID                  ID         `json:"id"`
	NodeID              ID         `json:"node_id"`
	BackendID           ID         `json:"backend_id"`
	Type                TunnelType `json:"type"`
	InterfaceName       string     `json:"interface_name"`
	LocalOverlayIP      string     `json:"local_overlay_ip"`
	RemoteOverlayIP     string     `json:"remote_overlay_ip"`
	ListenPort          int        `json:"listen_port,omitempty"`
	Endpoint            string     `json:"endpoint,omitempty"`
	AllowedIPs          []string   `json:"allowed_ips,omitempty"`
	PersistentKeepalive int        `json:"persistent_keepalive,omitempty"`
	Priority            int        `json:"priority"`
	PrivateKeyPath      string     `json:"private_key_path,omitempty"`
	PublicKey           string     `json:"public_key,omitempty"`
	ServiceName         string     `json:"service_name,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// Protocol is a L4 protocol for a public port mapping.
type Protocol string

const (
	ProtoTCP Protocol = "TCP"
	ProtoUDP Protocol = "UDP"
)

// PortMapping DNAT/forwards a public port on a node to a backend overlay port.
type PortMapping struct {
	ID          ID        `json:"id"`
	NodeID      ID        `json:"node_id"`
	BackendID   ID        `json:"backend_id"`
	Protocol    Protocol  `json:"protocol"`
	PublicPort  int       `json:"public_port"`
	BackendPort int       `json:"backend_port"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MappingPatch is the body of PATCH /api/v1/mappings/{id}.
type MappingPatch struct {
	Enabled *bool `json:"enabled"`
}

// SniMatch is one hostname rule (or the default) inside an SniRoute.
type SniMatch struct {
	Match     string `json:"match,omitempty"`
	Default   bool   `json:"default,omitempty"`
	BackendID ID     `json:"backend_id,omitempty"`
	Backend   string `json:"backend,omitempty"`
}

// SniRoute multiplexes TLS by SNI on a listen address.
type SniRoute struct {
	ID        ID         `json:"id"`
	NodeID    ID         `json:"node_id"`
	Listen    string     `json:"listen"`
	Matches   []SniMatch `json:"matches"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// HealthCheck is a probe the agent runs against a backend.
type HealthCheck struct {
	ID        ID            `json:"id"`
	BackendID ID            `json:"backend_id"`
	Protocol  Protocol      `json:"protocol"`
	Port      int           `json:"port"`
	Interval  time.Duration `json:"interval"`
	Timeout   time.Duration `json:"timeout"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// FailoverPolicy controls automatic WG→SSH failback. Fail-forward (SSH→WG)
// is never automatic unless AutomaticFailforward is explicitly true (default false).
type FailoverPolicy struct {
	ID                   ID            `json:"id"`
	NodeID               ID            `json:"node_id"`
	BackendID            ID            `json:"backend_id"`
	AutomaticFailback    bool          `json:"automatic_failback"`
	AutomaticFailforward bool          `json:"automatic_failforward"`
	CheckInterval        time.Duration `json:"check_interval"`
	FailureThreshold     int           `json:"failure_threshold"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

func DefaultFailoverPolicy() FailoverPolicy {
	return FailoverPolicy{
		AutomaticFailback:    true,
		AutomaticFailforward: false,
		CheckInterval:        10 * time.Second,
		FailureThreshold:     3,
	}
}

// TransportMode is the persisted overlay mode for a node/backend pair.
type TransportMode string

const (
	TransportWGPrimary          TransportMode = "WG_PRIMARY"
	TransportFailbackInProgress TransportMode = "FAILBACK_IN_PROGRESS"
	TransportSSHPrimary         TransportMode = "SSH_PRIMARY"
	TransportDegraded           TransportMode = "DEGRADED"
)

// TransportState is agent-local runtime state (also reported to the controller).
type TransportState struct {
	NodeID              ID            `json:"node_id"`
	BackendID           ID            `json:"backend_id"`
	State               TransportMode `json:"state"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	PrimaryHealthy      bool          `json:"primary_healthy"`
	FallbackHealthy     bool          `json:"fallback_healthy"`
	LastTransition      time.Time     `json:"last_transition"`
	LastReason          string        `json:"last_reason,omitempty"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

// AgentStatus is the last heartbeat from an edge agent.
type AgentStatus struct {
	NodeID          ID               `json:"node_id"`
	Healthy         bool             `json:"healthy"`
	LastReconcile   time.Time        `json:"last_reconcile,omitempty"`
	LastHeartbeat   time.Time        `json:"last_heartbeat"`
	Version         string           `json:"version"`
	TransportStates []TransportState `json:"transport_states,omitempty"`
}

// AuditEvent is an append-only record of mutating API/agent actions.
type AuditEvent struct {
	ID         ID        `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Success    bool      `json:"success"`
}

// Role is a coarse RBAC role stored on API tokens.
type Role string

const (
	RoleOperator Role = "operator"
	RoleReadonly Role = "readonly"
	RoleAgent    Role = "agent"
)

func ParseRole(s string) (Role, error) {
	switch Role(strings.ToLower(strings.TrimSpace(s))) {
	case RoleOperator:
		return RoleOperator, nil
	case RoleReadonly:
		return RoleReadonly, nil
	case RoleAgent:
		return RoleAgent, nil
	default:
		return "", Validation("role must be operator, readonly, or agent")
	}
}

// TokenCreateRequest is the operator-only mint payload.
type TokenCreateRequest struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
}

// TokenCreateResult returns plaintext once. It is never stored.
type TokenCreateResult struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// PrincipalView is the authenticated caller (no secret).
type PrincipalView struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
}

// Token is the DB row for a hashed credential. The plaintext is never stored.
type Token struct {
	ID        ID        `json:"id"`
	Name      string    `json:"name"`
	Hash      string    `json:"-"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}

// DesiredState is the full intended configuration for one edge node.
type DesiredState struct {
	Node             Node             `json:"node"`
	Backends         []Backend        `json:"backends"`
	Tunnels          []Tunnel         `json:"tunnels"`
	Mappings         []PortMapping    `json:"mappings"`
	SniRoutes        []SniRoute       `json:"sni_routes"`
	HealthChecks     []HealthCheck    `json:"health_checks"`
	FailoverPolicies []FailoverPolicy `json:"failover_policies"`
	FailbackIntents  []FailbackIntent `json:"failback_intents,omitempty"`
}

type FailbackIntent struct {
	ID        ID        `json:"id"`
	NodeID    ID        `json:"node_id"`
	BackendID ID        `json:"backend_id"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

// ActualState is what the agent last discovered (and reported) on a node.
type ActualState struct {
	NodeID          ID               `json:"node_id"`
	Tunnels         []TunnelActual   `json:"tunnels"`
	FirewallRules   []FirewallRule   `json:"firewall_rules"`
	HaproxyDigest   string           `json:"haproxy_digest,omitempty"`
	SniRoutes       []SniRoute       `json:"sni_routes,omitempty"`
	TransportStates []TransportState `json:"transport_states"`
	Conflicts       []Conflict       `json:"conflicts,omitempty"`
	DiscoveredAt    time.Time        `json:"discovered_at"`
}

// TunnelActual is discovered runtime facts for one overlay interface.
type TunnelActual struct {
	TunnelID         ID         `json:"tunnel_id,omitempty"`
	Type             TunnelType `json:"type"`
	InterfaceName    string     `json:"interface_name"`
	InterfacePresent bool       `json:"interface_present"`
	LocalOverlayIP   string     `json:"local_overlay_ip,omitempty"`
	Endpoint         string     `json:"endpoint,omitempty"`
	ListenPort       int        `json:"listen_port,omitempty"`
	PublicKey        string     `json:"public_key,omitempty"`
	HandshakeAgeSec  int64      `json:"handshake_age_seconds"`
	RxBytes          uint64     `json:"rx_bytes"`
	TxBytes          uint64     `json:"tx_bytes"`
	ServiceActive    bool       `json:"service_active,omitempty"`
}

// FirewallRule is one discovered managed (or colliding) NAT/filter rule.
type FirewallRule struct {
	Chain   string `json:"chain"`
	Comment string `json:"comment,omitempty"`
	Spec    string `json:"spec"`
	Managed bool   `json:"managed"`
}

// Conflict is a host condition the agent must not silently overwrite.
type Conflict struct {
	Code    string `json:"code"`
	Target  string `json:"target"`
	Message string `json:"message"`
}

// ApplyRequest is the body of POST .../apply.
type ApplyRequest struct {
	DryRun bool `json:"dry_run"`
}

// ApplyResult is returned by apply (dry-run or live).
type ApplyResult struct {
	DryRun  bool   `json:"dry_run"`
	Applied bool   `json:"applied"`
	Plan    string `json:"plan"`
	Message string `json:"message,omitempty"`
}

// TunnelStatus is a controller-side view of tunnel health from last actual-state.
type TunnelStatus struct {
	Tunnel  Tunnel        `json:"tunnel"`
	Actual  *TunnelActual `json:"actual,omitempty"`
	Healthy bool          `json:"healthy"`
	Detail  string        `json:"detail,omitempty"`
}

// NodeActualState is GET /api/v1/nodes/{id}/actual-state.
type NodeActualState struct {
	Actual *ActualState `json:"actual,omitempty"`
	Status *AgentStatus `json:"status,omitempty"`
}
