package sshtun

import (
	"context"
	"fmt"
	"strings"

	"transitforge/internal/cmdexec"
	"transitforge/internal/model"
	"transitforge/internal/validate"
)

// Manager inspects and starts an existing systemd OpenSSH TUN unit.
// It does not implement SSH, generate keys, or write sshd/ssh_config.
type Manager struct {
	Runner cmdexec.CommandRunner
}

func (m *Manager) Discover(ctx context.Context, t model.Tunnel) (model.TunnelActual, []model.Conflict, error) {
	act := model.TunnelActual{
		TunnelID:      t.ID,
		Type:          model.TunnelSSHTUN,
		InterfaceName: t.InterfaceName,
	}
	if err := t.Validate(); err != nil {
		return act, nil, err
	}
	if err := validate.ServiceName(t.ServiceName); err != nil {
		return act, nil, err
	}
	out, errb, err := m.Runner.Run(ctx, "systemctl", "is-active", t.ServiceName)
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		act.ServiceActive = true
	} else {
		_ = errb
	}
	_, errb, err = m.Runner.Run(ctx, "ip", "-o", "link", "show", "dev", t.InterfaceName)
	if err == nil {
		act.InterfacePresent = true
		addrOut, _, _ := m.Runner.Run(ctx, "ip", "-o", "-4", "addr", "show", "dev", t.InterfaceName)
		act.LocalOverlayIP = parseIPv4(string(addrOut))
	} else if !missing(err, string(errb)) {
		return act, nil, fmt.Errorf("ip link show %s: %w", t.InterfaceName, err)
	}
	return act, nil, nil
}

func (m *Manager) Apply(ctx context.Context, t model.Tunnel) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if t.Type != model.TunnelSSHTUN {
		return model.Validation("sshtun manager cannot apply " + string(t.Type))
	}
	_, errb, err := m.Runner.Run(ctx, "systemctl", "start", t.ServiceName)
	if err != nil {
		return fmt.Errorf("systemctl start %s: %w (%s)", t.ServiceName, err, strings.TrimSpace(string(errb)))
	}
	act, _, err := m.Discover(ctx, t)
	if err != nil {
		return err
	}
	if !act.ServiceActive {
		return model.ErrConflict("SSH TUN unit " + t.ServiceName + " is not active after start")
	}
	return nil
}

func parseIPv4(out string) string {
	fs := strings.Fields(out)
	for i, f := range fs {
		if f == "inet" && i+1 < len(fs) {
			ip := fs[i+1]
			if n := strings.IndexByte(ip, '/'); n >= 0 {
				ip = ip[:n]
			}
			return ip
		}
	}
	return ""
}

func missing(err error, stderr string) bool {
	s := strings.ToLower(err.Error() + " " + stderr)
	return strings.Contains(s, "cannot find device") || strings.Contains(s, "not found") || strings.Contains(s, "inactive")
}
