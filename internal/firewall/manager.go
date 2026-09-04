package firewall

import (
	"context"
	"fmt"

	"proxyctl/internal/model"
	"proxyctl/internal/reconcile"
)

const (
	TableNAT     = "nat"
	TableFilter  = "filter"
	ChainDNAT    = "PROXYCTL_DNAT"
	ChainForward = "PROXYCTL_FORWARD"
	ChainSNAT    = "PROXYCTL_SNAT"
	JumpComment  = "proxyctl:jump"
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
