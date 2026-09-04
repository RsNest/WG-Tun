package wireguard

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"proxyctl/internal/cmdexec"
	"proxyctl/internal/model"
	"proxyctl/internal/validate"
)

type Manager interface {
	Discover(ctx context.Context, desired []model.Tunnel) ([]model.TunnelActual, []model.Conflict, error)
	Apply(ctx context.Context, desired model.Tunnel) error
	Delete(ctx context.Context, iface string) error
	RollbackCreate(ctx context.Context, iface string) error
}

type WGManager struct {
	Runner        cmdexec.CommandRunner
	createdIfaces []string
}

func (m *WGManager) Discover(ctx context.Context, desired []model.Tunnel) ([]model.TunnelActual, []model.Conflict, error) {
	var out []model.TunnelActual
	var conflicts []model.Conflict
	seen := map[string]bool{}
	for _, t := range desired {
		if t.Type != model.TunnelWireGuard {
			continue
		}
		if err := validate.InterfaceName(t.InterfaceName); err != nil {
			return nil, nil, err
		}
		seen[t.InterfaceName] = true
		act, conf, err := m.inspect(ctx, t)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, act)
		conflicts = append(conflicts, conf...)
	}
	return out, conflicts, nil
}

func (m *WGManager) inspect(ctx context.Context, t model.Tunnel) (model.TunnelActual, []model.Conflict, error) {
	act := model.TunnelActual{
		TunnelID:      t.ID,
		Type:          model.TunnelWireGuard,
		InterfaceName: t.InterfaceName,
	}
	out, errb, err := m.Runner.Run(ctx, "ip", "-o", "link", "show", "dev", t.InterfaceName)
	if err != nil {
		if ifaceMissing(err, string(errb)) {
			return act, nil, nil
		}
		return act, nil, fmt.Errorf("ip link show %s: %w", t.InterfaceName, err)
	}
	act.InterfacePresent = true
	if strings.Contains(string(out), "state DOWN") || strings.Contains(string(out), "state UNKNOWN") {
		// still present
	}
	addrOut, _, addrErr := m.Runner.Run(ctx, "ip", "-o", "-4", "addr", "show", "dev", t.InterfaceName)
	if addrErr == nil {
		act.LocalOverlayIP = parseLocalIPv4(string(addrOut))
	}
	dump, dumpErrb, dumpErr := m.Runner.Run(ctx, "wg", "show", t.InterfaceName, "dump")
	if dumpErr != nil {
		if ifaceMissing(dumpErr, string(dumpErrb)) {
			return act, []model.Conflict{{
				Code:    "NOT_WIREGUARD",
				Target:  t.InterfaceName,
				Message: "interface exists but is not a WireGuard device",
			}}, nil
		}
		return act, nil, fmt.Errorf("wg show dump: %w", dumpErr)
	}
	parseWGDump(&act, string(dump))
	return act, nil, nil
}

func (m *WGManager) Apply(ctx context.Context, t model.Tunnel) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if t.Type != model.TunnelWireGuard {
		return model.Validation("WGManager cannot apply " + string(t.Type))
	}
	if t.PrivateKeyPath == "" {
		return model.ErrConflict("WIREGUARD tunnel " + t.InterfaceName + " requires private_key_path (file reference, not a raw key)")
	}
	if err := validate.PathRef(t.PrivateKeyPath); err != nil {
		return err
	}
	if err := validate.InterfaceName(t.InterfaceName); err != nil {
		return err
	}
	if err := validate.IPv4(t.LocalOverlayIP); err != nil {
		return err
	}
	if err := validate.Port(t.ListenPort); err != nil {
		return err
	}

	_, _, err := m.Runner.Run(ctx, "ip", "-o", "link", "show", "dev", t.InterfaceName)
	exists := err == nil
	if !exists {
		if _, errb, err := m.Runner.Run(ctx, "ip", "link", "add", "dev", t.InterfaceName, "type", "wireguard"); err != nil {
			return fmt.Errorf("ip link add %s: %w (%s)", t.InterfaceName, err, strings.TrimSpace(string(errb)))
		}
		m.createdIfaces = append(m.createdIfaces, t.InterfaceName)
	}

	cur, _, _ := m.Runner.Run(ctx, "ip", "-o", "-4", "addr", "show", "dev", t.InterfaceName)
	if parseLocalIPv4(string(cur)) != t.LocalOverlayIP {
		if _, errb, err := m.Runner.Run(ctx, "ip", "addr", "replace", t.LocalOverlayIP+"/32", "dev", t.InterfaceName); err != nil {
			return fmt.Errorf("ip addr replace: %w (%s)", err, strings.TrimSpace(string(errb)))
		}
	}

	if _, errb, err := m.Runner.Run(ctx, "wg", "set", t.InterfaceName, "listen-port", strconv.Itoa(t.ListenPort), "private-key", t.PrivateKeyPath); err != nil {
		return fmt.Errorf("wg set listen-port/private-key: %w (%s)", err, strings.TrimSpace(string(errb)))
	}
	if t.PublicKey != "" {
		args := []string{"set", t.InterfaceName, "peer", t.PublicKey}
		if len(t.AllowedIPs) > 0 {
			args = append(args, "allowed-ips", strings.Join(t.AllowedIPs, ","))
		}
		if t.Endpoint != "" {
			if err := validate.Endpoint(t.Endpoint); err != nil {
				return err
			}
			args = append(args, "endpoint", t.Endpoint)
		}
		if t.PersistentKeepalive > 0 {
			args = append(args, "persistent-keepalive", strconv.Itoa(t.PersistentKeepalive))
		}
		if _, errb, err := m.Runner.Run(ctx, "wg", args...); err != nil {
			return fmt.Errorf("wg set peer: %w (%s)", err, strings.TrimSpace(string(errb)))
		}
	}
	if _, errb, err := m.Runner.Run(ctx, "ip", "link", "set", t.InterfaceName, "up"); err != nil {
		return fmt.Errorf("ip link set up: %w (%s)", err, strings.TrimSpace(string(errb)))
	}
	return nil
}

func (m *WGManager) Delete(ctx context.Context, iface string) error {
	if err := validate.InterfaceName(iface); err != nil {
		return err
	}
	_, errb, err := m.Runner.Run(ctx, "ip", "link", "delete", "dev", iface)
	if err != nil && !ifaceMissing(err, string(errb)) {
		return fmt.Errorf("ip link delete %s: %w", iface, err)
	}
	return nil
}

func (m *WGManager) CreatedIfaces() []string {
	return append([]string{}, m.createdIfaces...)
}

func (m *WGManager) BeginTx() {
	m.createdIfaces = nil
}

func (m *WGManager) RollbackCreate(ctx context.Context, iface string) error {
	return m.Delete(ctx, iface)
}

func parseWGDump(act *model.TunnelActual, dump string) {
	lines := strings.Split(strings.TrimSpace(dump), "\n")
	if len(lines) == 0 {
		return
	}
	ifaceFields := strings.Fields(lines[0])
	if len(ifaceFields) >= 3 {
		act.PublicKey = ifaceFields[1]
		if p, err := strconv.Atoi(ifaceFields[2]); err == nil {
			act.ListenPort = p
		}
	}
	if len(lines) < 2 {
		return
	}
	peer := strings.Fields(lines[1])
	if len(peer) >= 8 {
		act.Endpoint = peer[2]
		if peer[2] == "(none)" {
			act.Endpoint = ""
		}
		hs, _ := strconv.ParseInt(peer[4], 10, 64)
		if hs > 0 {
			act.HandshakeAgeSec = int64(time.Since(time.Unix(hs, 0)).Seconds())
			if act.HandshakeAgeSec < 0 {
				act.HandshakeAgeSec = 0
			}
		}
		act.RxBytes, _ = strconv.ParseUint(peer[5], 10, 64)
		act.TxBytes, _ = strconv.ParseUint(peer[6], 10, 64)
	}
}

func parseLocalIPv4(out string) string {
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "inet" && i+1 < len(fields) {
			ip := fields[i+1]
			if slash := strings.IndexByte(ip, '/'); slash >= 0 {
				ip = ip[:slash]
			}
			return ip
		}
	}
	return ""
}

func ifaceMissing(err error, stderr string) bool {
	s := strings.ToLower(err.Error() + " " + stderr)
	return strings.Contains(s, "cannot find device") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "no such device") ||
		strings.Contains(s, "no such file")
}

func RedactWG(s string) string {
	if strings.Contains(strings.ToLower(s), "private") {
		return "[redacted]"
	}
	return s
}
