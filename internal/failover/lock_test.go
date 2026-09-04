package failover_test

import (
	"context"
	"os"
	"testing"

	"proxyctl/internal/failover"
	"proxyctl/internal/model"
)

func TestLockAndStateFile(t *testing.T) {
	dir := t.TempDir()
	l := failover.NewLock(dir)
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := failover.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Put(model.TransportState{NodeID: "n", BackendID: "b", State: model.TransportWGPrimary})
	if err := failover.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(failover.StatePath(dir)); err != nil {
		t.Fatal(err)
	}
	st2, err := failover.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st2.Get("n", "b")
	if got.State != model.TransportWGPrimary {
		t.Fatalf("%+v", got)
	}
}
