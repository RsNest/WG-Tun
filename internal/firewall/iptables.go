package firewall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"transitforge/internal/cmdexec"
	"transitforge/internal/model"
	"transitforge/internal/reconcile"
	"transitforge/internal/validate"
)

// IptablesNftManager manages dedicated TRANSITFORGE_* chains via the iptables CLI
// (nft-compatible iptables-nft included). It never flushes PREROUTING/FORWARD/POSTROUTING.
type IptablesNftManager struct {
	Runner    cmdexec.CommandRunner
	BackupDir string

	mu       sync.Mutex
	snapshot []byte
	lastAdd  []iptablesRule
	lastDel  []iptablesRule
}

type iptablesRule struct {
	Table   string
	Chain   string
	Args    []string
	Comment string
	Spec    string
}

func (m *IptablesNftManager) iptables(ctx context.Context, args ...string) ([]byte, error) {
	out, errb, err := m.Runner.Run(ctx, "iptables", args...)
	if err != nil {
		return out, fmt.Errorf("iptables %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(errb)))
	}
	return out, nil
}

type chainView struct{ table, chain string }

func iptablesChainViews() []chainView {
	return []chainView{
		{TableNAT, ChainDNAT},
		{TableFilter, ChainForward},
		{TableNAT, ChainSNAT},
		{TableNAT, legacyChainDNAT},
		{TableFilter, legacyChainForward},
		{TableNAT, legacyChainSNAT},
	}
}

func isDNATChain(chain string) bool {
	return chain == ChainDNAT || chain == legacyChainDNAT
}

func (m *IptablesNftManager) Discover(ctx context.Context) ([]model.FirewallRule, []model.Conflict, error) {
	var rules []model.FirewallRule
	var conflicts []model.Conflict
	for _, item := range iptablesChainViews() {
		out, err := m.iptables(ctx, "-t", item.table, "-S", item.chain)
		if err != nil {
			if chainMissing(err) {
				continue
			}
			return nil, nil, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "-N ") {
				continue
			}
			cmt := parseComment(line)
			managed := isManagedMappingComment(cmt)
			if cmt != "" && !managed && cmt != JumpComment && cmt != legacyJumpComment {
				conflicts = append(conflicts, model.Conflict{
					Code:    "UNMANAGED_RULE",
					Target:  item.chain,
					Message: "unmanaged rule in TransitForge chain: " + line,
				})
			}
			if managed && !isDNATChain(item.chain) {
				continue
			}
			spec := specFromSaveLine(line, cmt)
			rules = append(rules, model.FirewallRule{
				Chain:   item.chain,
				Comment: cmt,
				Spec:    spec,
				Managed: managed,
			})
		}
	}
	return rules, conflicts, nil
}

func (m *IptablesNftManager) Plan(desired []model.PortMapping, backends []model.Backend, actual []model.FirewallRule) reconcile.Plan {
	ds := model.DesiredState{Mappings: desired, Backends: backends}
	st := model.ActualState{FirewallRules: actual}
	p := reconcile.Diff(ds, st)
	fw := reconcile.Plan{}
	for _, a := range p.Actions {
		if a.Target == "firewall" {
			fw.Actions = append(fw.Actions, a)
		}
	}
	fw.Conflicts = p.Conflicts
	return fw
}

func (m *IptablesNftManager) Apply(ctx context.Context, plan reconcile.Plan, desired []model.PortMapping, backends []model.Backend) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if plan.HasConflicts() {
		return model.ErrConflict(plan.String())
	}
	if err := m.backupLocked(ctx); err != nil {
		return err
	}
	if err := m.ensureInfrastructure(ctx); err != nil {
		return err
	}
	want := map[string]model.PortMapping{}
	for _, mp := range desired {
		want[MappingComment(mp.ID)] = mp
	}
	m.lastAdd = nil
	m.lastDel = nil

	for _, act := range plan.Actions {
		if act.Target != "firewall" {
			continue
		}
		switch act.Kind {
		case reconcile.KindAdd, reconcile.KindChange:
			mp, ok := want[MappingComment(model.ID(act.ID))]
			if !ok {
				return fmt.Errorf("apply firewall: mapping %s missing from desired", act.ID)
			}
			addr := BackendAddr(mp, backends)
			if addr == "" {
				return model.Validation("firewall apply: backend address missing for mapping " + act.ID)
			}
			if err := validate.IPv4(addr); err != nil {
				return err
			}
			if err := validate.Port(mp.PublicPort); err != nil {
				return err
			}
			if err := validate.Port(mp.BackendPort); err != nil {
				return err
			}
			if act.Kind == reconcile.KindChange {
				if err := m.deleteByComment(ctx, MappingComment(mp.ID)); err != nil {
					return err
				}
			}
			added, err := m.addMappingRules(ctx, mp, addr)
			if err != nil {
				return err
			}
			m.lastAdd = append(m.lastAdd, added...)
		case reconcile.KindDelete:
			cmt := commentFromSpec(act.Before)
			if cmt == "" {
				cmt = act.ID
			}
			deleted, err := m.deleteByCommentCopy(ctx, cmt)
			if err != nil {
				return err
			}
			m.lastDel = append(m.lastDel, deleted...)
		}
	}
	return nil
}

func (m *IptablesNftManager) Rollback(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.snapshot) == 0 && len(m.lastAdd) == 0 && len(m.lastDel) == 0 {
		return nil
	}
	for i := len(m.lastAdd) - 1; i >= 0; i-- {
		r := m.lastAdd[i]
		args := append([]string{"-t", r.Table, "-D", r.Chain}, r.Args...)
		if _, err := m.iptables(ctx, args...); err != nil {
			if m.snapshot != nil {
				return m.restoreSnapshot(ctx)
			}
			return err
		}
	}
	for _, r := range m.lastDel {
		args := append([]string{"-t", r.Table, "-A", r.Chain}, r.Args...)
		if _, err := m.iptables(ctx, args...); err != nil {
			if m.snapshot != nil {
				return m.restoreSnapshot(ctx)
			}
			return err
		}
	}
	m.lastAdd = nil
	m.lastDel = nil
	return nil
}

func (m *IptablesNftManager) Counters(ctx context.Context) ([]Counters, error) {
	var out []Counters
	for _, item := range iptablesChainViews() {
		b, err := m.iptables(ctx, "-t", item.table, "-L", item.chain, "-n", "-v", "-x")
		if err != nil {
			if chainMissing(err) {
				continue
			}
			return nil, err
		}
		out = append(out, parseCounters(item.chain, string(b)))
	}
	return out, nil
}

func (m *IptablesNftManager) backupLocked(ctx context.Context) error {
	out, errb, err := m.Runner.Run(ctx, "iptables-save")
	if err != nil {
		return fmt.Errorf("iptables-save: %w (%s)", err, strings.TrimSpace(string(errb)))
	}
	m.snapshot = append([]byte{}, out...)
	if m.BackupDir == "" {
		return nil
	}
	if err := os.MkdirAll(m.BackupDir, 0o750); err != nil {
		return err
	}
	name := filepath.Join(m.BackupDir, "iptables-"+time.Now().UTC().Format("20060102T150405Z")+".save")
	return os.WriteFile(name, out, 0o640)
}

func (m *IptablesNftManager) restoreSnapshot(ctx context.Context) error {
	if m.snapshot == nil {
		return fmt.Errorf("no iptables snapshot")
	}
	_, errb, err := m.Runner.RunStdin(ctx, m.snapshot, "iptables-restore")
	if err != nil {
		return fmt.Errorf("iptables-restore: %w (%s)", err, strings.TrimSpace(string(errb)))
	}
	return nil
}

func (m *IptablesNftManager) ensureInfrastructure(ctx context.Context) error {
	type jump struct {
		table, parent, chain string
	}
	jumps := []jump{
		{TableNAT, "PREROUTING", ChainDNAT},
		{TableFilter, "FORWARD", ChainForward},
		{TableNAT, "POSTROUTING", ChainSNAT},
	}
	for _, j := range jumps {
		if _, err := m.iptables(ctx, "-t", j.table, "-S", j.chain); err != nil {
			if !chainMissing(err) {
				return err
			}
			if _, err := m.iptables(ctx, "-t", j.table, "-N", j.chain); err != nil {
				return err
			}
		}
		_, err := m.iptables(ctx, "-t", j.table, "-C", j.parent, "-m", "comment", "--comment", JumpComment, "-j", j.chain)
		if err != nil {
			if _, err := m.iptables(ctx, "-t", j.table, "-A", j.parent, "-m", "comment", "--comment", JumpComment, "-j", j.chain); err != nil {
				return err
			}
		}
	}
	m.dropLegacyChains(ctx)
	return nil
}

func (m *IptablesNftManager) dropLegacyChains(ctx context.Context) {
	type jump struct{ table, parent, chain, comment string }
	for _, j := range []jump{
		{TableNAT, "PREROUTING", legacyChainDNAT, legacyJumpComment},
		{TableFilter, "FORWARD", legacyChainForward, legacyJumpComment},
		{TableNAT, "POSTROUTING", legacyChainSNAT, legacyJumpComment},
	} {
		_, _ = m.iptables(ctx, "-t", j.table, "-D", j.parent, "-m", "comment", "--comment", j.comment, "-j", j.chain)
		_, _ = m.iptables(ctx, "-t", j.table, "-F", j.chain)
		_, _ = m.iptables(ctx, "-t", j.table, "-X", j.chain)
	}
}

func (m *IptablesNftManager) addMappingRules(ctx context.Context, mp model.PortMapping, addr string) ([]iptablesRule, error) {
	proto := strings.ToLower(string(mp.Protocol))
	cmt := MappingComment(mp.ID)
	dnatArgs := []string{
		"-p", proto, "-m", proto, "--dport", strconv.Itoa(mp.PublicPort),
		"-m", "comment", "--comment", cmt,
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", addr, mp.BackendPort),
	}
	fwdArgs := []string{
		"-p", proto, "-m", proto, "-d", addr, "--dport", strconv.Itoa(mp.BackendPort),
		"-m", "comment", "--comment", cmt,
		"-j", "ACCEPT",
	}
	snatArgs := []string{
		"-p", proto, "-m", proto, "-d", addr, "--dport", strconv.Itoa(mp.BackendPort),
		"-m", "comment", "--comment", cmt,
		"-j", "MASQUERADE",
	}
	rules := []iptablesRule{
		{Table: TableNAT, Chain: ChainDNAT, Args: dnatArgs, Comment: cmt, Spec: MappingSpec(mp, addr)},
		{Table: TableFilter, Chain: ChainForward, Args: fwdArgs, Comment: cmt, Spec: MappingSpec(mp, addr)},
		{Table: TableNAT, Chain: ChainSNAT, Args: snatArgs, Comment: cmt, Spec: MappingSpec(mp, addr)},
	}
	for _, r := range rules {
		args := append([]string{"-t", r.Table, "-A", r.Chain}, r.Args...)
		if _, err := m.iptables(ctx, args...); err != nil {
			return nil, err
		}
	}
	return rules, nil
}

func (m *IptablesNftManager) deleteByComment(ctx context.Context, cmt string) error {
	_, err := m.deleteByCommentCopy(ctx, cmt)
	return err
}

func (m *IptablesNftManager) deleteByCommentCopy(ctx context.Context, cmt string) ([]iptablesRule, error) {
	var deleted []iptablesRule
	for _, item := range iptablesChainViews() {
		out, err := m.iptables(ctx, "-t", item.table, "-S", item.chain)
		if err != nil {
			if chainMissing(err) {
				continue
			}
			return deleted, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if parseComment(line) != cmt || !strings.HasPrefix(line, "-A ") {
				continue
			}
			args := splitArgs(line)
			if len(args) < 2 {
				continue
			}
			ruleArgs := args[2:]
			del := append([]string{"-t", item.table, "-D", item.chain}, ruleArgs...)
			if _, err := m.iptables(ctx, del...); err != nil {
				return deleted, err
			}
			deleted = append(deleted, iptablesRule{Table: item.table, Chain: item.chain, Args: ruleArgs, Comment: cmt})
		}
	}
	return deleted, nil
}

func chainMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no chain") || strings.Contains(s, "does not exist")
}

func parseComment(line string) string {
	const needle = "--comment "
	i := strings.Index(line, needle)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[i+len(needle):])
	if strings.HasPrefix(rest, `"`) {
		rest = strings.TrimPrefix(rest, `"`)
		j := strings.Index(rest, `"`)
		if j >= 0 {
			return rest[:j]
		}
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func specFromSaveLine(line, cmt string) string {
	if !isManagedMappingComment(cmt) {
		return line
	}
	proto := ""
	dport := 0
	dest := ""
	fields := strings.Fields(line)
	for i, f := range fields {
		switch f {
		case "-p":
			if i+1 < len(fields) {
				proto = strings.ToUpper(fields[i+1])
			}
		case "--dport":
			if i+1 < len(fields) {
				dport, _ = strconv.Atoi(fields[i+1])
			}
		case "--to-destination":
			if i+1 < len(fields) {
				dest = fields[i+1]
			}
		}
	}
	if proto != "" && dport > 0 && dest != "" {
		return fmt.Sprintf("%s dport %d -> %s comment %s", proto, dport, dest, cmt)
	}
	return line
}

func commentFromSpec(spec string) string {
	const p = "comment "
	i := strings.LastIndex(spec, p)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(spec[i+len(p):])
}

func splitArgs(line string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQ = !inQ
		case c == ' ' && !inQ:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func parseCounters(chain, body string) Counters {
	c := Counters{Chain: chain}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Chain") || strings.HasPrefix(line, "pkts") {
			continue
		}
		fs := strings.Fields(line)
		if len(fs) < 2 {
			continue
		}
		pkts, _ := strconv.ParseUint(fs[0], 10, 64)
		byt, _ := strconv.ParseUint(fs[1], 10, 64)
		c.Rules = append(c.Rules, RuleCounter{Packets: pkts, Bytes: byt, Spec: line, Comment: parseComment(line)})
	}
	return c
}
