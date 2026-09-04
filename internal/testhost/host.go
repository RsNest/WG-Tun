package testhost

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"transitforge/internal/cmdexec"
)

// Host is an in-memory iptables/ip/wg simulator for tests. It implements CommandRunner.
type Host struct {
	mu          sync.Mutex
	NAT         map[string][]string // chain -> -S lines without the leading "-A chain"
	Filter      map[string][]string
	Links       map[string]Link
	FailOn      string
	Calls       [][]string
	Units       map[string]string
	Unreachable map[string]bool
}

type Link struct {
	Name      string
	Kind      string
	Up        bool
	Addrs     []string
	Listen    int
	PrivPath  string
	PublicKey string
	PeerKey   string
	Endpoint  string
	Allowed   string
	Keepalive int
	Handshake int64
	Rx, Tx    uint64
}

func New() *Host {
	return &Host{
		NAT:         map[string][]string{"PREROUTING": nil, "POSTROUTING": nil},
		Filter:      map[string][]string{"FORWARD": nil, "INPUT": nil, "OUTPUT": nil},
		Links:       map[string]Link{},
		Units:       map[string]string{},
		Unreachable: map[string]bool{},
	}
}

func (h *Host) Run(_ context.Context, executable string, args ...string) ([]byte, []byte, error) {
	return h.exec(nil, executable, args)
}

func (h *Host) RunStdin(_ context.Context, stdin []byte, executable string, args ...string) ([]byte, []byte, error) {
	return h.exec(stdin, executable, args)
}

func (h *Host) exec(stdin []byte, executable string, args []string) ([]byte, []byte, error) {
	if err := cmdexec.ValidateExecutable(executable); err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Calls = append(h.Calls, append([]string{executable}, args...))
	joined := executable + " " + strings.Join(args, " ")
	if h.FailOn != "" && strings.Contains(joined, h.FailOn) {
		fail := h.FailOn
		h.FailOn = ""
		return nil, []byte("injected failure"), fmt.Errorf("injected failure: %s", fail)
	}
	switch executable {
	case "iptables":
		return h.iptables(args)
	case "iptables-save":
		return h.save(), nil, nil
	case "iptables-restore":
		return nil, nil, h.restore(stdin)
	case "ip":
		return h.ip(args)
	case "wg":
		return h.wg(args)
	case "haproxy":
		return h.haproxy(args)
	case "systemctl":
		return h.systemctl(args)
	case "ping":
		return h.ping(args)
	default:
		return nil, nil, fmt.Errorf("unexpected executable %s", executable)
	}
}

func (h *Host) iptables(args []string) ([]byte, []byte, error) {
	table := "filter"
	rest := args
	if len(rest) >= 2 && rest[0] == "-t" {
		table = rest[1]
		rest = rest[2:]
	}
	if len(rest) == 0 {
		return nil, nil, fmt.Errorf("iptables: no command")
	}
	chains := h.table(table)
	switch rest[0] {
	case "-N":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -N needs chain")
		}
		if _, ok := chains[rest[1]]; ok {
			return nil, []byte("Chain already exists"), fmt.Errorf("exists")
		}
		chains[rest[1]] = nil
		return nil, nil, nil
	case "-S":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -S needs chain")
		}
		rules, ok := chains[rest[1]]
		if !ok {
			return nil, []byte("iptables: No chain/target/match by that name."), fmt.Errorf("no chain")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "-N %s\n", rest[1])
		for _, r := range rules {
			fmt.Fprintf(&b, "-A %s %s\n", rest[1], r)
		}
		return []byte(b.String()), nil, nil
	case "-A":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -A")
		}
		chain := rest[1]
		if _, ok := chains[chain]; !ok {
			return nil, []byte("No chain/target/match by that name."), fmt.Errorf("no chain")
		}
		rule := strings.Join(rest[2:], " ")
		chains[chain] = append(chains[chain], rule)
		return nil, nil, nil
	case "-D":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -D")
		}
		chain := rest[1]
		want := strings.Join(rest[2:], " ")
		list := chains[chain]
		for i, r := range list {
			if r == want {
				chains[chain] = append(list[:i], list[i+1:]...)
				return nil, nil, nil
			}
		}
		return nil, []byte("Bad rule"), fmt.Errorf("no matching rule")
	case "-C":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -C")
		}
		chain := rest[1]
		want := strings.Join(rest[2:], " ")
		for _, r := range chains[chain] {
			if r == want {
				return nil, nil, nil
			}
		}
		return nil, []byte("Bad rule"), fmt.Errorf("check failed")
	case "-L":
		if len(rest) < 2 {
			return nil, nil, fmt.Errorf("iptables -L")
		}
		chain := rest[1]
		rules, ok := chains[chain]
		if !ok {
			return nil, []byte("No chain/target/match by that name."), fmt.Errorf("no chain")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Chain %s (0 references)\n pkts bytes target\n", chain)
		for _, r := range rules {
			fmt.Fprintf(&b, "    0     0 %s\n", r)
		}
		return []byte(b.String()), nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported iptables op %s", rest[0])
	}
}

func (h *Host) table(name string) map[string][]string {
	if name == "nat" {
		return h.NAT
	}
	return h.Filter
}

func (h *Host) save() []byte {
	var b bytes.Buffer
	b.WriteString("*nat\n")
	for chain, rules := range h.NAT {
		fmt.Fprintf(&b, ":%s - [0:0]\n", chain)
		for _, r := range rules {
			fmt.Fprintf(&b, "-A %s %s\n", chain, r)
		}
	}
	b.WriteString("COMMIT\n*filter\n")
	for chain, rules := range h.Filter {
		fmt.Fprintf(&b, ":%s - [0:0]\n", chain)
		for _, r := range rules {
			fmt.Fprintf(&b, "-A %s %s\n", chain, r)
		}
	}
	b.WriteString("COMMIT\n")
	return b.Bytes()
}

func (h *Host) restore(stdin []byte) error {
	h.NAT = map[string][]string{}
	h.Filter = map[string][]string{}
	table := ""
	for _, line := range strings.Split(string(stdin), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "*nat":
			table = "nat"
		case line == "*filter":
			table = "filter"
		case strings.HasPrefix(line, ":"):
			name := strings.Fields(strings.TrimPrefix(line, ":"))[0]
			if table == "nat" {
				h.NAT[name] = nil
			} else if table == "filter" {
				h.Filter[name] = nil
			}
		case strings.HasPrefix(line, "-A "):
			fs := strings.SplitN(line, " ", 3)
			if len(fs) < 3 {
				continue
			}
			if table == "nat" {
				h.NAT[fs[1]] = append(h.NAT[fs[1]], fs[2])
			} else if table == "filter" {
				h.Filter[fs[1]] = append(h.Filter[fs[1]], fs[2])
			}
		}
	}
	return nil
}

func (h *Host) ip(args []string) ([]byte, []byte, error) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		i++
	}
	if i >= len(args) {
		return nil, nil, fmt.Errorf("ip: no args")
	}
	switch args[i] {
	case "link":
		return h.ipLink(args[i+1:])
	case "addr":
		return h.ipAddr(args[i+1:])
	default:
		return nil, nil, fmt.Errorf("ip: unsupported %v", args)
	}
}

func (h *Host) ipAddr(args []string) ([]byte, []byte, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("ip addr")
	}
	switch args[0] {
	case "replace", "add":
		cidr := args[1]
		dev := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "dev" && i+1 < len(args) {
				dev = args[i+1]
			}
		}
		l, ok := h.Links[dev]
		if !ok {
			return nil, []byte("Cannot find device"), fmt.Errorf("cannot find device")
		}
		l.Addrs = []string{cidr}
		h.Links[dev] = l
		return nil, nil, nil
	case "show":
		dev := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "dev" && i+1 < len(args) {
				dev = args[i+1]
			}
		}
		l, ok := h.Links[dev]
		if !ok {
			return nil, []byte("Cannot find device"), fmt.Errorf("cannot find device")
		}
		var b strings.Builder
		for _, a := range l.Addrs {
			fmt.Fprintf(&b, "1: %s    inet %s\n", l.Name, a)
		}
		return []byte(b.String()), nil, nil
	default:
		return nil, nil, fmt.Errorf("ip addr %v", args)
	}
}

func (h *Host) ipLink(args []string) ([]byte, []byte, error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("ip link")
	}
	switch args[0] {
	case "add":
		name, kind := "", ""
		for i := 0; i < len(args); i++ {
			if args[i] == "dev" && i+1 < len(args) {
				name = args[i+1]
			}
			if args[i] == "type" && i+1 < len(args) {
				kind = args[i+1]
			}
		}
		if name == "" {
			return nil, nil, fmt.Errorf("ip link add: no dev")
		}
		if _, ok := h.Links[name]; ok {
			return nil, []byte("File exists"), fmt.Errorf("exists")
		}
		h.Links[name] = Link{Name: name, Kind: kind}
		return nil, nil, nil
	case "delete", "del":
		name := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "dev" && i+1 < len(args) {
				name = args[i+1]
			}
		}
		if name == "" && len(args) > 1 {
			name = args[1]
		}
		if _, ok := h.Links[name]; !ok {
			return nil, []byte("Cannot find device"), fmt.Errorf("cannot find device")
		}
		delete(h.Links, name)
		return nil, nil, nil
	case "set":
		name := args[1]
		l, ok := h.Links[name]
		if !ok {
			return nil, []byte("Cannot find device"), fmt.Errorf("cannot find device")
		}
		if contains(args, "up") {
			l.Up = true
		}
		h.Links[name] = l
		return nil, nil, nil
	case "show":
		name := ""
		for i := 0; i < len(args); i++ {
			if args[i] == "dev" && i+1 < len(args) {
				name = args[i+1]
			}
		}
		if name == "" {
			var b strings.Builder
			for _, l := range h.Links {
				st := "DOWN"
				if l.Up {
					st = "UP"
				}
				fmt.Fprintf(&b, "1: %s: <...,state %s>\n", l.Name, st)
			}
			return []byte(b.String()), nil, nil
		}
		l, ok := h.Links[name]
		if !ok {
			return nil, []byte("Cannot find device"), fmt.Errorf("cannot find device")
		}
		st := "DOWN"
		if l.Up {
			st = "UP"
		}
		return []byte(fmt.Sprintf("1: %s: <...,state %s>\n", l.Name, st)), nil, nil
	default:
		return nil, nil, fmt.Errorf("ip link %v", args)
	}
}

func (h *Host) wg(args []string) ([]byte, []byte, error) {
	if len(args) < 2 || args[0] != "show" && args[0] != "set" {
		if len(args) > 0 && args[0] == "set" {
			return h.wgSet(args[1:])
		}
		return nil, nil, fmt.Errorf("wg %v", args)
	}
	if args[0] == "set" {
		return h.wgSet(args[1:])
	}
	iface := args[1]
	l, ok := h.Links[iface]
	if !ok || l.Kind != "wireguard" {
		return nil, []byte("Unable to access interface: No such device"), fmt.Errorf("no such device")
	}
	if len(args) >= 3 && args[2] == "dump" {
		priv := "0000000000000000000000000000000000000000000000000000000000000000"
		pub := l.PublicKey
		if pub == "" {
			pub = "1111111111111111111111111111111111111111111111111111111111111111"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s %s %d off\n", priv, pub, l.Listen)
		if l.PeerKey != "" {
			ep := l.Endpoint
			if ep == "" {
				ep = "(none)"
			}
			fmt.Fprintf(&b, "%s (none) %s %s %d %d %d %d\n", l.PeerKey, ep, emptyDash(l.Allowed), l.Handshake, l.Rx, l.Tx, l.Keepalive)
		}
		return []byte(b.String()), nil, nil
	}
	return nil, nil, fmt.Errorf("wg show %v", args)
}

func (h *Host) wgSet(args []string) ([]byte, []byte, error) {
	if len(args) < 1 {
		return nil, nil, fmt.Errorf("wg set")
	}
	iface := args[0]
	l, ok := h.Links[iface]
	if !ok {
		return nil, []byte("No such device"), fmt.Errorf("no such device")
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "listen-port":
			i++
			l.Listen, _ = strconv.Atoi(args[i])
		case "private-key":
			i++
			l.PrivPath = args[i]
		case "peer":
			i++
			l.PeerKey = args[i]
		case "allowed-ips":
			i++
			l.Allowed = args[i]
		case "endpoint":
			i++
			l.Endpoint = args[i]
		case "persistent-keepalive":
			i++
			l.Keepalive, _ = strconv.Atoi(args[i])
		}
	}
	h.Links[iface] = l
	return nil, nil, nil
}

func (h *Host) haproxy(args []string) ([]byte, []byte, error) {
	if len(args) >= 1 && args[0] == "-c" {
		return []byte("Configuration file is valid\n"), nil, nil
	}
	return nil, nil, fmt.Errorf("haproxy %v", args)
}

func (h *Host) systemctl(args []string) ([]byte, []byte, error) {
	if len(args) < 2 {
		return nil, nil, fmt.Errorf("systemctl %v", args)
	}
	switch args[0] {
	case "reload":
		return nil, nil, nil
	case "restart":
		return nil, nil, fmt.Errorf("restart is forbidden")
	case "is-active":
		st := h.Units[args[1]]
		if st == "" {
			st = "inactive"
		}
		if st != "active" {
			return []byte(st + "\n"), nil, fmt.Errorf("inactive")
		}
		return []byte("active\n"), nil, nil
	case "start":
		h.Units[args[1]] = "active"
		return nil, nil, nil
	case "show":
		st := h.Units[args[len(args)-1]]
		if st == "" {
			st = "inactive"
		}
		return []byte("ActiveState=" + st + "\n"), nil, nil
	default:
		return nil, nil, fmt.Errorf("systemctl %v", args)
	}
}

func (h *Host) ping(args []string) ([]byte, []byte, error) {
	ip := args[len(args)-1]
	if h.Unreachable[ip] {
		return nil, []byte("100% packet loss"), fmt.Errorf("ping failed")
	}
	return []byte("1 packets transmitted, 1 received\n"), nil, nil
}

func emptyDash(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func contains(ss []string, n string) bool {
	for _, s := range ss {
		if s == n {
			return true
		}
	}
	return false
}

func (h *Host) MappingRuleCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.NAT["TRANSITFORGE_DNAT"] {
		if strings.Contains(r, "transitforge:mapping:") {
			n++
		}
	}
	return n
}
