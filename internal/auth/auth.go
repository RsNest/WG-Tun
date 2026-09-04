package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"proxyctl/internal/model"
	"proxyctl/internal/store"
)

const (
	HeaderTimestamp = "X-Proxyctl-Timestamp"
	HeaderSignature = "X-Proxyctl-Signature"
	bootstrapName   = "bootstrap-operator"
)

type Principal struct {
	Name string
	Role model.Role
}

type Authenticator struct {
	store   store.Store
	hmacReq bool
	maxSkew time.Duration
	cost    int
}

func New(st store.Store, hmacRequired bool, maxSkew time.Duration) *Authenticator {
	if maxSkew == 0 {
		maxSkew = 5 * time.Minute
	}
	return &Authenticator{store: st, hmacReq: hmacRequired, maxSkew: maxSkew, cost: bcrypt.DefaultCost}
}

func (a *Authenticator) EnsureBootstrapToken(ctx context.Context, tokenFile string) (created bool, err error) {
	ok, err := a.store.HasTokenName(ctx, bootstrapName)
	if err != nil {
		return false, err
	}
	if ok {
		if _, err := os.Stat(tokenFile); err != nil {
			return false, fmt.Errorf("bootstrap token exists in the database but %s is missing; restore the original token file (it cannot be recovered from the hash)", tokenFile)
		}
		return false, nil
	}
	var plain string
	if b, err := os.ReadFile(tokenFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		plain = strings.TrimSpace(string(b))
	} else {
		plain, err = randomToken()
		if err != nil {
			return false, err
		}
		if err := os.MkdirAll(filepath.Dir(tokenFile), 0o750); err != nil {
			return false, err
		}
		if err := os.WriteFile(tokenFile, []byte(plain+"\n"), 0o600); err != nil {
			return false, fmt.Errorf("write bootstrap token: %w", err)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), a.cost)
	if err != nil {
		return false, err
	}
	t := &model.Token{
		ID:        model.ID(newID()),
		Name:      bootstrapName,
		Hash:      string(hash),
		Role:      model.RoleOperator,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.store.CreateToken(ctx, t); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Authenticator) CreateToken(ctx context.Context, name string, role model.Role) (plaintext string, t *model.Token, err error) {
	plain, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), a.cost)
	if err != nil {
		return "", nil, err
	}
	tok := &model.Token{
		ID:        model.ID(newID()),
		Name:      name,
		Hash:      string(hash),
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.store.CreateToken(ctx, tok); err != nil {
		return "", nil, err
	}
	return plain, tok, nil
}

func (a *Authenticator) Authenticate(r *http.Request, body []byte) (*Principal, error) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		return nil, model.Unauthorized("missing bearer token")
	}
	token := strings.TrimSpace(raw[7:])
	if token == "" {
		return nil, model.Unauthorized("empty bearer token")
	}
	tok, err := a.matchToken(r.Context(), token)
	if err != nil {
		return nil, err
	}
	if a.hmacReq {
		if err := a.verifyHMAC(r, body, token); err != nil {
			return nil, err
		}
	}
	return &Principal{Name: tok.Name, Role: tok.Role}, nil
}

func (a *Authenticator) matchToken(ctx context.Context, plaintext string) (*model.Token, error) {
	tokens, err := a.store.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tokens {
		t := &tokens[i]
		if t.Revoked {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(t.Hash), []byte(plaintext)) == nil {
			return t, nil
		}
	}
	return nil, model.Unauthorized("invalid token")
}

func (a *Authenticator) verifyHMAC(r *http.Request, body []byte, token string) error {
	ts := strings.TrimSpace(r.Header.Get(HeaderTimestamp))
	sig := strings.TrimSpace(r.Header.Get(HeaderSignature))
	if ts == "" || sig == "" {
		return model.Unauthorized("missing HMAC headers")
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return model.Unauthorized("invalid HMAC timestamp")
	}
	t := time.Unix(unix, 0)
	if d := time.Since(t); d > a.maxSkew || d < -a.maxSkew {
		return model.Unauthorized("HMAC timestamp outside allowed skew")
	}
	sum := sha256.Sum256(body)
	canonical := ts + "\n" + r.Method + "\n" + r.URL.Path + "\n" + hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(strings.ToLower(want))) {
		return model.Unauthorized("invalid HMAC signature")
	}
	return nil
}

func CanMutate(role model.Role) bool {
	return role == model.RoleOperator || role == model.RoleAgent
}

func CanRead(role model.Role) bool {
	return role == model.RoleOperator || role == model.RoleReadonly || role == model.RoleAgent
}

func Sign(token, method, path string, body []byte, now time.Time) (ts, sig string) {
	ts = strconv.FormatInt(now.Unix(), 10)
	sum := sha256.Sum256(body)
	canonical := ts + "\n" + method + "\n" + path + "\n" + hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	return ts, hex.EncodeToString(mac.Sum(nil))
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var idMu sync.Mutex
var idCounter uint64

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		idMu.Lock()
		idCounter++
		n := idCounter
		idMu.Unlock()
		return fmt.Sprintf("tok-%d-%d", time.Now().UnixNano(), n)
	}
	return hex.EncodeToString(b)
}
