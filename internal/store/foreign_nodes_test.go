package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"transitforge/internal/model"
	"transitforge/internal/store"
)

func TestForeignNodePersistsWithoutChangingDesiredState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	s, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	node := &model.Node{ID: "entry", Name: "entry"}
	if err := s.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	backend := &model.Backend{ID: "backend", NodeID: node.ID, Name: "backend", Address: "10.0.0.2"}
	if err := s.CreateBackend(ctx, backend); err != nil {
		t.Fatal(err)
	}
	n := &model.ForeignNode{ID: "foreign", ForeignNodeInput: model.ForeignNodeInput{Name: "foreign", PublicAddress: "example.net", Labels: map[string]string{"location": "EU"}}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.CreateForeignNode(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetForeignNode(ctx, n.ID)
	if err != nil || got.Labels["location"] != "EU" {
		t.Fatal("foreign node did not persist")
	}
	ds, err := s.DesiredState(ctx, node.ID)
	if err != nil || len(ds.Backends) != 1 || ds.Backends[0].ID != backend.ID {
		t.Fatal("existing desired state changed")
	}
}
