package health

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"transitforge/internal/cmdexec"
	"transitforge/internal/model"
	"transitforge/internal/validate"
)

type DialFunc func(ctx context.Context, network, address string) error

type Checker struct {
	Runner cmdexec.CommandRunner
	Dial   DialFunc
	Now    func() time.Time
}

type Snapshot struct {
	InterfacePresent bool
	UnitActive       bool
	OverlayReachable bool
	HandshakeAgeSec  int64
	ProbesTotal      int
	ProbesFailed     int
}

func (c *Checker) OverlayPing(ctx context.Context, ip string) (bool, error) {
	if err := validate.IPv4(ip); err != nil {
		return false, err
	}
	_, errb, err := c.Runner.Run(ctx, "ping", "-c", "1", "-W", "2", ip)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()+" "+string(errb)), "injected") {
			return false, nil
		}
		return false, nil
	}
	return true, nil
}

func (c *Checker) ProbeBackend(ctx context.Context, addr string, proto model.Protocol, port int) (bool, error) {
	if err := validate.IPv4(addr); err != nil {
		return false, err
	}
	if err := validate.Port(port); err != nil {
		return false, err
	}
	dial := c.Dial
	if dial == nil {
		dial = defaultDial
	}
	network := "tcp"
	if proto == model.ProtoUDP {
		network = "udp"
	}
	err := dial(ctx, network, net.JoinHostPort(addr, strconv.Itoa(port)))
	return err == nil, nil
}

func defaultDial(ctx context.Context, network, address string) error {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	return conn.Close()
}

func PrimaryFailed(h Snapshot, probeNeed int) bool {
	if !h.InterfacePresent {
		return true
	}
	probesBad := h.ProbesTotal > 0 && h.ProbesFailed >= probeNeed
	if h.ProbesTotal > 0 && h.ProbesFailed == h.ProbesTotal {
		probesBad = true
	}
	hsStale := h.HandshakeAgeSec > 180
	overlayBad := !h.OverlayReachable
	if probesBad && (hsStale || overlayBad) {
		return true
	}
	return false
}

func FallbackHealthy(h Snapshot) bool {
	return h.UnitActive && h.InterfacePresent && h.OverlayReachable && h.ProbesFailed == 0
}

func (s Snapshot) String() string {
	return fmt.Sprintf("iface=%t unit=%t overlay=%t hs=%d probes=%d/%d",
		s.InterfacePresent, s.UnitActive, s.OverlayReachable, s.HandshakeAgeSec, s.ProbesTotal-s.ProbesFailed, s.ProbesTotal)
}
