package validate

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

var (
	ifaceNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,14}$`)
	tokenNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	hostNameRe  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
	dnsLabelRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	unitNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,255}$`)
)

func errf(format string, args ...any) error {
	return fmt.Errorf("VALIDATION: "+format, args...)
}

func NodeName(name string) error {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return errf("node name is required")
	}
	if len(n) > 63 || !dnsLabelRe.MatchString(n) {
		return errf("node name must be a lowercase DNS label (max 63)")
	}
	return nil
}

func BackendName(name string) error {
	return NodeName(name)
}

func TokenName(name string) error {
	if !tokenNameRe.MatchString(name) {
		return errf("token name must be alphanumeric with ._- (max 63)")
	}
	return nil
}

func Username(name string) error {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return errf("username is required")
	}
	if len(n) > 63 || !dnsLabelRe.MatchString(n) {
		return errf("username must be a lowercase DNS label (max 63)")
	}
	return nil
}

func HumanPassword(pw string) error {
	if strings.TrimSpace(pw) == "" {
		return errf("password is required")
	}
	if len(pw) < 10 {
		return errf("password must be at least 10 characters")
	}
	if len(pw) > 200 {
		return errf("password is too long")
	}
	return nil
}

func IPv4(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errf("ip address is required")
	}
	ip, err := netip.ParseAddr(s)
	if err != nil || !ip.Is4() {
		return errf("invalid IPv4 address %q", s)
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return errf("IPv4 address %q is not usable", s)
	}
	return nil
}

func OptionalIPv4(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return IPv4(s)
}

func CIDR(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errf("CIDR is required")
	}
	if _, _, err := net.ParseCIDR(s); err != nil {
		return errf("invalid CIDR %q", s)
	}
	return nil
}

func Port(p int) error {
	if p < 1 || p > 65535 {
		return errf("port %d out of range 1-65535", p)
	}
	return nil
}

func OptionalPort(p int) error {
	if p == 0 {
		return nil
	}
	return Port(p)
}

func InterfaceName(name string) error {
	if !ifaceNameRe.MatchString(name) {
		return errf("invalid interface name %q", name)
	}
	return nil
}

func Protocol(p string) error {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "TCP", "UDP":
		return nil
	default:
		return errf("protocol must be TCP or UDP, got %q", p)
	}
}

func TunnelType(t string) error {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "WIREGUARD", "SSH_TUN":
		return nil
	default:
		return errf("tunnel type must be WIREGUARD or SSH_TUN, got %q", t)
	}
}

func Endpoint(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return errf("endpoint must be host:port, got %q", s)
	}
	if host == "" {
		return errf("endpoint host is required")
	}
	if ip := net.ParseIP(host); ip == nil {
		if err := Hostname(host); err != nil {
			return err
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return errf("invalid endpoint port %q", port)
	}
	return Port(n)
}

func Hostname(s string) error {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 253 || !hostNameRe.MatchString(s) {
		return errf("invalid hostname %q", s)
	}
	return nil
}

func ListenAddr(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errf("listen address is required")
	}
	if strings.HasPrefix(s, ":") {
		n, err := strconv.Atoi(s[1:])
		if err != nil {
			return errf("invalid listen port in %q", s)
		}
		return Port(n)
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return errf("listen must be host:port or :port, got %q", s)
	}
	if host != "" && net.ParseIP(host) == nil {
		if err := Hostname(host); err != nil {
			return err
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return errf("invalid listen port %q", port)
	}
	return Port(n)
}

func OverlayPair(local, remote string) error {
	if err := IPv4(local); err != nil {
		return errf("local_overlay_ip: %s", strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	if err := IPv4(remote); err != nil {
		return errf("remote_overlay_ip: %s", strings.TrimPrefix(err.Error(), "VALIDATION: "))
	}
	if local == remote {
		return errf("local and remote overlay IPs must differ")
	}
	return nil
}

func PathRef(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	if strings.Contains(p, "\x00") || strings.ContainsAny(p, "\n\r") {
		return errf("path contains illegal characters")
	}
	return nil
}

func ServiceName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !unitNameRe.MatchString(s) {
		return errf("invalid systemd unit name %q", s)
	}
	return nil
}

func Wrap(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimPrefix(err.Error(), "VALIDATION: ")
	return errf("%s", msg)
}
