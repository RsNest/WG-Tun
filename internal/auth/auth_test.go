package auth_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"transitforge/internal/auth"
	"transitforge/internal/store"
)

func TestHMACRoundTrip(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := auth.New(st, true, 5*time.Minute)
	tokFile := filepath.Join(t.TempDir(), "tok")
	if _, err := a.EnsureBootstrapToken(context.Background(), tokFile); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(tokFile)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"name":"x"}`)
	ts, sig := auth.Sign(string(bytesTrim(plain)), http.MethodPost, "/api/v1/nodes", body, time.Now())
	req, _ := http.NewRequest(http.MethodPost, "http://x/api/v1/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+string(bytesTrim(plain)))
	req.Header.Set(auth.HeaderTimestamp, ts)
	req.Header.Set(auth.HeaderSignature, sig)
	if _, err := a.Authenticate(req, body); err != nil {
		t.Fatal(err)
	}
	req.Header.Set(auth.HeaderSignature, "00")
	if _, err := a.Authenticate(req, body); err == nil {
		t.Fatal("bad sig")
	}
}

func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
