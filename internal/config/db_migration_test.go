package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"transitforge/internal/config"
	"transitforge/internal/model"
	"transitforge/internal/store"
)

// Legacy names here are migration fixtures, not current product names.
func TestDBRenamePreservesInventoryAndMFAWithWAL(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source.db")
	st, err := store.OpenSQLite(source)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	node := &model.Node{ID: "node-1", Name: "edge-1"}
	if err := st.CreateNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "user-1", Username: "admin", Role: model.RoleAdministrator, Locale: "ru", PasswordHash: "fixture-hash", TOTPSecret: "fixture-secret", TOTPEnabled: true}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRecoveryCodes(ctx, user.ID, []string{"fixture-recovery"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	legacy := filepath.Join(dir, "proxyctl.db")
	// Snapshot the idle writer's files before close/checkpoint, leaving real WAL data.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(source + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacy+suffix, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := config.ResolveDBPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := store.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if got, err := migrated.GetNode(ctx, node.ID); err != nil || got.Name != node.Name {
		t.Fatal("inventory lost during filename migration")
	}
	got, err := migrated.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != user.PasswordHash || got.TOTPSecret != user.TOTPSecret || !got.TOTPEnabled || got.Role != user.Role {
		t.Fatal("account authentication changed during filename migration")
	}
	codes, err := migrated.ListRecoveryCodeHashes(ctx, user.ID)
	if err != nil || len(codes) != 1 || codes[0].Hash != "fixture-recovery" {
		t.Fatal("recovery codes changed during filename migration")
	}
}

func TestDBRenameRefusesConflictingFiles(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		t.Run("destination"+suffix, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "proxyctl.db")
			destination := filepath.Join(dir, "transitforge.db") + suffix
			for _, path := range []string{old, old + "-wal", destination} {
				if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := config.ResolveDBPath(dir); err == nil {
				t.Fatal("conflicting destination accepted")
			}
			for _, path := range []string{old, old + "-wal", destination} {
				b, err := os.ReadFile(path)
				if err != nil || string(b) != "keep" {
					t.Fatal("migration modified a conflicting file")
				}
			}
		})
	}
}
