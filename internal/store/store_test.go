package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxyctl/internal/ident"
	"proxyctl/internal/model"
	"proxyctl/internal/store"
)

func TestSQLiteCRUD(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	n := &model.Node{ID: ident.New(), Name: "ru-edge-1", PublicIP: "203.0.113.10", CreatedAt: time.Now(), UpdatedAt: time.Now(), Labels: map[string]string{"role": "edge"}}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	b := &model.Backend{ID: ident.New(), Name: "backend-a", NodeID: n.ID, Address: "10.200.1.2", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.CreateBackend(ctx, b); err != nil {
		t.Fatal(err)
	}
	m := &model.PortMapping{ID: ident.New(), NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.CreateMapping(ctx, m); err != nil {
		t.Fatal(err)
	}
	ds, err := st.DesiredState(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ds.Node.Name != "ru-edge-1" || len(ds.Backends) != 1 || len(ds.Mappings) != 1 {
		t.Fatalf("desired: %+v", ds)
	}
	if err := st.CreateMapping(ctx, &model.PortMapping{ID: ident.New(), NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 80, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err == nil {
		t.Fatal("duplicate mapping should conflict")
	}
	if err := st.DeleteMapping(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledMappingOmittedFromDesired(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	n := &model.Node{ID: ident.New(), Name: "ru-edge-1", PublicIP: "203.0.113.10", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	b := &model.Backend{ID: ident.New(), Name: "backend-a", NodeID: n.ID, Address: "10.200.1.2", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.CreateBackend(ctx, b); err != nil {
		t.Fatal(err)
	}
	m := &model.PortMapping{ID: ident.New(), NodeID: n.ID, BackendID: b.ID, Protocol: model.ProtoTCP, PublicPort: 443, BackendPort: 443, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.CreateMapping(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMapping(ctx, m.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("new mapping should be enabled: %+v %v", got, err)
	}
	got.Enabled = false
	got.UpdatedAt = time.Now()
	if err := st.UpdateMapping(ctx, got); err != nil {
		t.Fatal(err)
	}
	ds, err := st.DesiredState(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Mappings) != 0 {
		t.Fatalf("disabled mapping must be omitted from desired-state: %+v", ds.Mappings)
	}
	listed, err := st.ListMappings(ctx)
	if err != nil || len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("catalog should still list disabled mapping: %+v %v", listed, err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}
