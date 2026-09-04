package validate_test

import (
	"testing"

	"transitforge/internal/validate"
)

func TestIPv4(t *testing.T) {
	if err := validate.IPv4("10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "999.1.1.1", "0.0.0.0", "224.0.0.1", "not-an-ip", "2001:db8::1"} {
		if err := validate.IPv4(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestPort(t *testing.T) {
	if err := validate.Port(443); err != nil {
		t.Fatal(err)
	}
	if err := validate.Port(0); err == nil {
		t.Fatal("port 0")
	}
	if err := validate.Port(70000); err == nil {
		t.Fatal("port 70000")
	}
}

func TestInterfaceName(t *testing.T) {
	if err := validate.InterfaceName("wg0"); err != nil {
		t.Fatal(err)
	}
	if err := validate.InterfaceName("wg-a"); err != nil {
		t.Fatal(err)
	}
	if err := validate.InterfaceName("bad name"); err == nil {
		t.Fatal("space should fail")
	}
	if err := validate.InterfaceName("thisnameiswaytoolong"); err == nil {
		t.Fatal("long name should fail")
	}
}

func TestCIDRAndEndpoint(t *testing.T) {
	if err := validate.CIDR("10.200.1.2/32"); err != nil {
		t.Fatal(err)
	}
	if err := validate.Endpoint("198.51.100.20:51820"); err != nil {
		t.Fatal(err)
	}
	if err := validate.Endpoint("host-only"); err == nil {
		t.Fatal("endpoint without port")
	}
}

func TestListenAddr(t *testing.T) {
	if err := validate.ListenAddr(":443"); err != nil {
		t.Fatal(err)
	}
	if err := validate.ListenAddr("127.0.0.1:443"); err != nil {
		t.Fatal(err)
	}
}
