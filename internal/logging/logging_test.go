package logging_test

import (
	"strings"
	"testing"

	"transitforge/internal/logging"
)

func TestRedactSecrets(t *testing.T) {
	if logging.Redact("hello") != "hello" {
		t.Fatal("plain text")
	}
	if logging.Redact("-----BEGIN PRIVATE KEY-----") != "[redacted]" {
		t.Fatal("private key")
	}
	if !strings.Contains(logging.Redact("ok"), "ok") {
		t.Fatal("ok")
	}
}
