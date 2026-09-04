package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunHealthcheckOK(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := runHealthcheck([]string{"-url", srv.URL + "/readyz", "-k"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunHealthcheckRejectsBadStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if err := runHealthcheck([]string{"-url", srv.URL + "/readyz", "-insecure"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunHealthcheckRequiresSkipVerifyForSelfSigned(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{} //nolint:gosec
	srv.StartTLS()
	t.Cleanup(srv.Close)
	if err := runHealthcheck([]string{"-url", srv.URL + "/readyz"}); err == nil {
		t.Fatal("expected TLS verify failure without -k")
	}
}
