package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestTOTPKnownVector(t *testing.T) {
	// RFC 6238 appendix B, SHA-1, 1970-01-01 + 59s → 287082.
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	ts := time.Unix(59, 0).UTC()
	if !verifyTOTP(secret, "287082", ts) {
		key, _ := decodeTOTPSecret(secret)
		t.Fatalf("expected RFC vector, got %s", totpAt(key, ts))
	}
	if verifyTOTP(secret, "000000", ts) {
		t.Fatal("wrong code must fail")
	}
}
