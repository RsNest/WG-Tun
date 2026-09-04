package version

import "testing"

func TestLineFallback(t *testing.T) {
	got := Line("proxctl")
	if got != "proxctl dev (unknown)" {
		t.Fatalf("got %q", got)
	}
}

func TestLineInjected(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() {
		Version, Commit = oldV, oldC
	})
	Version = "v1.2.3"
	Commit = "abc1234"
	if Line("proxyctl-controller") != "proxyctl-controller v1.2.3 (abc1234)" {
		t.Fatalf("got %q", Line("proxyctl-controller"))
	}
}
