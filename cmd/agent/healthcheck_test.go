package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentHealthcheckOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := runHealthcheck([]string{"--url", srv.URL + "/healthz"}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentHealthcheckBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	if err := runHealthcheck([]string{"--url", srv.URL + "/healthz"}); err == nil {
		t.Fatal("expected error")
	}
}
