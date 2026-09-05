package auth_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transitforge/internal/auth"
	"transitforge/internal/store"
)

func TestRevokedBootstrapSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath, tokenPath := filepath.Join(dir, "test.db"), filepath.Join(dir, "bootstrap.token")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	a := auth.New(st, true, time.Minute)
	if _, err := a.EnsureBootstrapToken(ctx, tokenPath); err != nil {
		t.Fatal(err)
	}
	tokens, err := st.ListTokens(ctx)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens=%v err=%v", tokens, err)
	}
	if err := st.RevokeToken(ctx, tokens[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	a = auth.New(st, true, time.Minute)
	if created, err := a.EnsureBootstrapToken(ctx, tokenPath); err != nil || created {
		t.Fatalf("bootstrap recreated=%v err=%v", created, err)
	}
	plain, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(plain))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/api/v1/whoami", nil)
	if err != nil {
		t.Fatal(err)
	}
	ts, sig := auth.Sign(token, req.Method, req.URL.Path, nil, time.Now())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(auth.HeaderTimestamp, ts)
	req.Header.Set(auth.HeaderSignature, sig)
	if _, err := a.Authenticate(req, nil); err == nil {
		t.Fatal("revoked bootstrap authenticated after restart")
	}
}
