package webui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"proxyctl/internal/model"
)

const (
	cookieName   = "proxyctl_ui"
	localeCookie = "proxyctl_locale"
	sessionTTL   = 12 * time.Hour
	localeTTL    = 365 * 24 * time.Hour
)

type session struct {
	ID          string
	Token       string
	UserID      model.ID
	Name        string
	DisplayName string
	Role        model.Role
	Locale      string
	MFAPending  bool
	Expires     time.Time
	FlashOK     string
	FlashErr    string
	FlashErrRaw string
}

type sessionStore struct {
	mu     sync.Mutex
	secret []byte
	byID   map[string]*session
	now    func() time.Time
}

func newSessionStore(now func() time.Time) (*sessionStore, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &sessionStore{secret: secret, byID: map[string]*session{}, now: now}, nil
}

func (s *sessionStore) put(in session) (*session, error) {
	idRaw := make([]byte, 16)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, err
	}
	in.ID = hex.EncodeToString(idRaw)
	in.Expires = s.now().UTC().Add(sessionTTL)
	if in.Locale == "" {
		in.Locale = "en"
	}
	s.mu.Lock()
	s.byID[in.ID] = &in
	s.mu.Unlock()
	return &in, nil
}

func (s *sessionStore) get(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[id]
	if sess == nil {
		return nil
	}
	if !sess.Expires.After(s.now().UTC()) {
		delete(s.byID, id)
		return nil
	}
	return sess
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

func (s *sessionStore) setFlash(id, ok, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.byID[id]; sess != nil {
		sess.FlashOK = ok
		sess.FlashErr = errMsg
		sess.FlashErrRaw = ""
	}
}

func (s *sessionStore) setFlashRaw(id, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.byID[id]; sess != nil {
		sess.FlashOK = ""
		sess.FlashErr = ""
		sess.FlashErrRaw = errMsg
	}
}

func (s *sessionStore) takeFlash(id string) (ok, errMsg, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[id]
	if sess == nil {
		return "", "", ""
	}
	ok, errMsg, raw = sess.FlashOK, sess.FlashErr, sess.FlashErrRaw
	sess.FlashOK, sess.FlashErr, sess.FlashErrRaw = "", "", ""
	return ok, errMsg, raw
}

func (s *sessionStore) updateLocale(id, locale string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.byID[id]; sess != nil {
		sess.Locale = locale
	}
}

func (s *sessionStore) clearMFA(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.byID[id]; sess != nil {
		sess.MFAPending = false
	}
}

func (s *sessionStore) sign(id string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(id))
	return id + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *sessionStore) verify(value string) string {
	id, sig, ok := splitCookie(value)
	if !ok {
		return ""
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(id))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return ""
	}
	return id
}

func splitCookie(v string) (id, sig string, ok bool) {
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '.' {
			return v[:i], v[i+1:], i > 0 && i+1 < len(v)
		}
	}
	return "", "", false
}

func (s *Server) sessionFrom(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	id := s.sessions.verify(c.Value)
	if id == "" {
		return nil
	}
	return s.sessions.get(id)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sess *session) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sessions.sign(sess.ID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
		MaxAge:   -1,
	})
}

func canWrite(role model.Role) bool {
	return role == model.RoleAdministrator || role == model.RoleOperator
}

func canAdmin(role model.Role) bool {
	return role == model.RoleAdministrator
}
