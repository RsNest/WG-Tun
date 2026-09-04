package firewall

import (
	"context"
	"fmt"
	"strings"

	"transitforge/internal/model"
	"transitforge/internal/reconcile"
)

const (
	TableNAT     = "nat"
	TableFilter  = "filter"
	ChainDNAT    = "TRANSITFORGE_DNAT"
	ChainForward = "TRANSITFORGE_FORWARD"
	ChainSNAT    = "TRANSITFORGE_SNAT"
	JumpComment  = "transitforge:jump"
	// Pre-rebrand iptables names. Discover/delete still recognize them; new writes use the TRANSITFORGE_* names.
	legacyChainDNAT    = "PROXYCTL_DNAT"
	legacyChainForward = "PROXYCTL_FORWARD"
	legacyChainSNAT    = "PROXYCTL_SNAT"
	legacyJumpComment  = "proxyctl:jump"
)

type Counters struct {
	Chain string
	Rules []RuleCounter
}

type RuleCounter struct {
	Comment string
	Packets uint64
	Bytes   uint64
	Spec    string
}

type Manager interface {
	Discover(ctx context.Context) ([]model.FirewallRule, []model.Conflict, error)
	Plan(desired []model.PortMapping, backends []model.Backend, actual []model.FirewallRule) reconcile.Plan
	Apply(ctx context.Context, plan reconcile.Plan, desired []model.PortMapping, backends []model.Backend) error
	Rollback(ctx context.Context) error
	Counters(ctx context.Context) ([]Counters, error)
}

func MappingComment(id model.ID) string {
	return reconcile.MappingComment(id)
}

func isManagedMappingComment(cmt string) bool {
	return strings.HasPrefix(cmt, "transitforge:mapping:") || strings.HasPrefix(cmt, "proxyctl:mapping:")
}

func MappingSpec(m model.PortMapping, backendAddr string) string {
	return fmt.Sprintf("%s dport %d -> %s:%d comment %s", m.Protocol, m.PublicPort, backendAddr, m.BackendPort, MappingComment(m.ID))
}

func BackendAddr(m model.PortMapping, backends []model.Backend) string {
	for _, b := range backends {
		if b.ID == m.BackendID {
			return b.Address
		}
	}
	return ""
}
