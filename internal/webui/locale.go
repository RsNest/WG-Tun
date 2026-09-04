package webui

import (
	"context"
	"net/http"
	"strings"

	"transitforge/internal/webui/i18n"
)

func (s *Server) locale(r *http.Request) string {
	if sess := s.sessionFrom(r); sess != nil && !sess.MFAPending && sess.Locale != "" {
		return i18n.Normalize(sess.Locale)
	}
	if c, err := r.Cookie(localeCookie); err == nil && strings.TrimSpace(c.Value) != "" {
		return i18n.Normalize(c.Value)
	}
	return i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"))
}

func (s *Server) setLocaleCookie(w http.ResponseWriter, locale string) {
	http.SetCookie(w, &http.Cookie{
		Name:     localeCookie,
		Value:    i18n.Normalize(locale),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
		MaxAge:   int(localeTTL.Seconds()),
	})
}

func (s *Server) postLocale(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	loc := i18n.Normalize(firstNonEmpty(r.FormValue("locale"), r.FormValue("lang")))
	s.setLocaleCookie(w, loc)
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.updateLocale(sess.ID, loc)
		if s.accounts != nil && sess.UserID != "" && !sess.MFAPending {
			_ = s.accounts.SetLocale(r.Context(), sess.UserID, loc)
		}
	}
	if hx(r) {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	next := r.Header.Get("Referer")
	if next == "" {
		next = "/"
		if s.sessionFrom(r) == nil {
			next = "/login"
		}
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) T(r *http.Request) func(string) string {
	return i18n.Translator(s.locale(r))
}

func (s *Server) needsSetup() bool {
	if s.accounts == nil {
		return false
	}
	n, err := s.accounts.Count(context.Background())
	if err != nil {
		return false
	}
	return n == 0
}
