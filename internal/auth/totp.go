package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1
)

func randomTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
}

func totpAt(secret []byte, t time.Time) string {
	counter := uint64(t.Unix() / totpPeriod)
	return hotp(secret, counter, totpDigits)
}

func hotp(secret []byte, counter uint64, digits int) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := int(sum[offset]&0x7f)<<24 | int(sum[offset+1])<<16 | int(sum[offset+2])<<8 | int(sum[offset+3])
	mod := 1
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

func verifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	key, err := decodeTOTPSecret(secret)
	if err != nil || len(key) == 0 {
		return false
	}
	for w := -totpSkew; w <= totpSkew; w++ {
		want := totpAt(key, now.Add(time.Duration(w)*totpPeriod*time.Second))
		if hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

func otpAuthURL(issuer, username, secret string) string {
	label := url.PathEscape(issuer + ":" + username)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
