package webui

import (
	"net/http"
	"strings"

	"proxyctl/internal/model"
	"proxyctl/internal/webui/i18n"
)

func (s *Server) usersList(w http.ResponseWriter, r *http.Request) {
	s.renderUsers(w, r, emptyForm(), "")
}

func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	var users []model.User
	if s.accounts != nil {
		var err error
		users, err = s.accounts.List(r.Context())
		if err != nil {
			s.pageErr(w, r, err)
			return
		}
	}
	var tokens []model.Token
	if tok, err := s.api(r).ListTokens(r.Context()); err == nil {
		tokens = tok
	}
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "users.title"), "users")
	p.Data = map[string]any{
		"Users":     users,
		"Tokens":    tokens,
		"Form":      form,
		"FormError": formErr,
	}
	s.render(w, r, "users", p)
}

func (s *Server) userCreate(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderUsers(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	_, err := s.accounts.Create(r.Context(), model.UserCreateRequest{
		Username:    r.FormValue("username"),
		DisplayName: r.FormValue("display_name"),
		Password:    r.FormValue("password"),
		Role:        model.Role(r.FormValue("role")),
		Locale:      r.FormValue("locale"),
	})
	if err != nil {
		s.renderUsers(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.user_created", "")
	s.redirect(w, r, "/users")
}

func (s *Server) userUpdate(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderUsers(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	id := model.ID(r.PathValue("id"))
	role := model.Role(strings.TrimSpace(r.FormValue("role")))
	locale := r.FormValue("locale")
	disabled := r.FormValue("disabled") == "true" || r.FormValue("disabled") == "on"
	patch := model.UserPatch{Role: &role, Locale: &locale, Disabled: &disabled}
	if dn := r.FormValue("display_name"); dn != "" {
		patch.DisplayName = &dn
	}
	if pw := r.FormValue("password"); strings.TrimSpace(pw) != "" {
		patch.Password = &pw
	}
	if _, err := s.accounts.Patch(r.Context(), id, patch); err != nil {
		s.renderUsers(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.user_updated", "")
	s.redirect(w, r, "/users")
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, nil, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, extra map[string]any, formErr string) {
	p := s.pageBase(r, i18n.T(s.locale(r), "settings.title"), "settings")
	data := map[string]any{"FormError": formErr, "User": nil}
	if extra != nil {
		for k, v := range extra {
			data[k] = v
		}
	}
	sess := s.sessionFrom(r)
	if s.accounts != nil && sess != nil && sess.UserID != "" {
		if u, err := s.accounts.Get(r.Context(), sess.UserID); err == nil {
			data["User"] = u
		}
	}
	p.Data = data
	s.render(w, r, "settings", p)
}

func (s *Server) settingsPassword(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || sess.UserID == "" || s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	_ = r.ParseForm()
	pw := r.FormValue("password")
	if _, err := s.accounts.Patch(r.Context(), sess.UserID, model.UserPatch{Password: &pw}); err != nil {
		s.renderSettings(w, r, nil, s.apiErr(err))
		return
	}
	s.flash(r, "flash.password_changed", "")
	s.redirect(w, r, "/settings")
}

func (s *Server) settingsTOTPBegin(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || sess.UserID == "" || s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	out, err := s.accounts.BeginTOTP(r.Context(), sess.UserID)
	if err != nil {
		s.renderSettings(w, r, nil, s.apiErr(err))
		return
	}
	s.renderSettings(w, r, map[string]any{"TOTP": out}, "")
}

func (s *Server) settingsTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || sess.UserID == "" || s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	_ = r.ParseForm()
	out, err := s.accounts.ConfirmTOTP(r.Context(), sess.UserID, r.FormValue("code"))
	if err != nil {
		s.renderSettings(w, r, nil, s.apiErr(err))
		return
	}
	s.flash(r, "flash.totp_enabled", "")
	s.renderSettings(w, r, map[string]any{"Recovery": out}, "")
}

func (s *Server) settingsTOTPDisable(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || sess.UserID == "" || s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	if err := s.accounts.DisableTOTP(r.Context(), sess.UserID); err != nil {
		s.renderSettings(w, r, nil, s.apiErr(err))
		return
	}
	s.flash(r, "flash.totp_disabled", "")
	s.redirect(w, r, "/settings")
}

func (s *Server) settingsRecovery(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || sess.UserID == "" || s.accounts == nil {
		s.writeForbidden(w, r)
		return
	}
	out, err := s.accounts.RotateRecoveryCodes(r.Context(), sess.UserID)
	if err != nil {
		s.renderSettings(w, r, nil, s.apiErr(err))
		return
	}
	s.flash(r, "flash.recovery_generated", "")
	s.renderSettings(w, r, map[string]any{"Recovery": out}, "")
}

func (s *Server) apiReference(w http.ResponseWriter, r *http.Request) {
	p := s.pageBase(r, i18n.T(s.locale(r), "apiref.title"), "apiref")
	p.Data = apiReferenceRows()
	s.render(w, r, "apiref", p)
}

type apiRefRow struct {
	Method     string
	Path       string
	AuthKey    string
	PurposeKey string
}

func apiReferenceRows() []apiRefRow {
	return []apiRefRow{
		{"GET", "/healthz", "authz.none", "apiref.p.healthz"},
		{"GET", "/readyz", "authz.none", "apiref.p.readyz"},
		{"GET", "/metrics", "authz.none", "apiref.p.metrics"},
		{"GET", "/api/v1/whoami", "authz.read", "apiref.p.whoami"},
		{"GET", "/api/v1/tokens", "authz.operator", "apiref.p.tokens_list"},
		{"POST", "/api/v1/tokens", "authz.operator", "apiref.p.tokens_mint"},
		{"GET", "/api/v1/users", "authz.admin", "apiref.p.users_list"},
		{"POST", "/api/v1/users", "authz.admin", "apiref.p.users_create"},
		{"GET", "/api/v1/users/{id}", "authz.admin", "apiref.p.users_get"},
		{"PATCH", "/api/v1/users/{id}", "authz.admin", "apiref.p.users_patch"},
		{"POST", "/api/v1/users/{id}/totp/begin", "authz.admin", "apiref.p.totp_begin"},
		{"POST", "/api/v1/users/{id}/totp/confirm", "authz.admin", "apiref.p.totp_confirm"},
		{"POST", "/api/v1/users/{id}/totp/disable", "authz.admin", "apiref.p.totp_disable"},
		{"POST", "/api/v1/users/{id}/recovery-codes", "authz.admin", "apiref.p.recovery"},
		{"GET", "/api/v1/nodes", "authz.read", "apiref.p.nodes_list"},
		{"POST", "/api/v1/nodes", "authz.write", "apiref.p.nodes_create"},
		{"GET", "/api/v1/nodes/{id}", "authz.read", "apiref.p.nodes_get"},
		{"GET", "/api/v1/nodes/{id}/desired-state", "authz.read", "apiref.p.desired"},
		{"GET", "/api/v1/nodes/{id}/actual-state", "authz.read", "apiref.p.actual_get"},
		{"POST", "/api/v1/nodes/{id}/actual-state", "authz.write", "apiref.p.actual_post"},
		{"GET", "/api/v1/nodes/{id}/plan", "authz.read", "apiref.p.plan"},
		{"POST", "/api/v1/nodes/{id}/apply", "authz.write", "apiref.p.apply"},
		{"POST", "/api/v1/nodes/{id}/failback", "authz.write", "apiref.p.failback"},
		{"GET", "/api/v1/backends", "authz.read", "apiref.p.backends_list"},
		{"POST", "/api/v1/backends", "authz.write", "apiref.p.backends_create"},
		{"GET", "/api/v1/backends/{id}", "authz.read", "apiref.p.backends_get"},
		{"PATCH", "/api/v1/backends/{id}", "authz.write", "apiref.p.backends_patch"},
		{"GET", "/api/v1/mappings", "authz.read", "apiref.p.mappings_list"},
		{"POST", "/api/v1/mappings", "authz.write", "apiref.p.mappings_create"},
		{"PATCH", "/api/v1/mappings/{id}", "authz.write", "apiref.p.mappings_patch"},
		{"DELETE", "/api/v1/mappings/{id}", "authz.write", "apiref.p.mappings_delete"},
		{"GET", "/api/v1/tunnels", "authz.read", "apiref.p.tunnels_list"},
		{"POST", "/api/v1/tunnels", "authz.write", "apiref.p.tunnels_create"},
		{"GET", "/api/v1/tunnels/{id}/status", "authz.read", "apiref.p.tunnels_status"},
		{"GET", "/api/v1/sni-routes", "authz.read", "apiref.p.sni_list"},
		{"POST", "/api/v1/sni-routes", "authz.write", "apiref.p.sni_create"},
		{"GET", "/api/v1/sni-routes/{id}", "authz.read", "apiref.p.sni_get"},
		{"PATCH", "/api/v1/sni-routes/{id}", "authz.write", "apiref.p.sni_patch"},
		{"GET", "/api/v1/events", "authz.read", "apiref.p.events"},
	}
}
