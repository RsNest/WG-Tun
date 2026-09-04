package webui

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"transitforge/internal/auth"
	"transitforge/internal/logging"
	"transitforge/internal/model"
	"transitforge/internal/webui/i18n"
)

type Config struct {
	Listen       string
	API          http.Handler
	NewClient    func(token string) API
	Auth         *auth.Authenticator
	Accounts     *auth.Accounts
	Log          *logging.Logger
	CookieSecure bool
	Now          func() time.Time
	LiveApply    bool
}

type Server struct {
	listen       string
	log          *logging.Logger
	newClient    func(token string) API
	apiHandler   http.Handler
	auth         *auth.Authenticator
	accounts     *auth.Accounts
	sessions     *sessionStore
	tmpl         *template.Template
	static       fs.FS
	cookieSecure bool
	now          func() time.Time
	liveApply    bool
}

func New(cfg Config) (*Server, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	sessions, err := newSessionStore(cfg.Now)
	if err != nil {
		return nil, err
	}
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(content, "static")
	if err != nil {
		return nil, err
	}
	s := &Server{
		listen:       cfg.Listen,
		log:          cfg.Log,
		newClient:    cfg.NewClient,
		apiHandler:   cfg.API,
		auth:         cfg.Auth,
		accounts:     cfg.Accounts,
		sessions:     sessions,
		tmpl:         tmpl,
		static:       static,
		cookieSecure: cfg.CookieSecure,
		now:          cfg.Now,
		liveApply:    cfg.LiveApply,
	}
	if s.newClient == nil {
		if cfg.API == nil {
			return nil, fmt.Errorf("webui: API handler or NewClient is required")
		}
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))))
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("GET /login/mfa", s.getMFA)
	mux.HandleFunc("POST /login/mfa", s.postMFA)
	mux.HandleFunc("GET /setup", s.getSetup)
	mux.HandleFunc("POST /setup", s.postSetup)
	mux.HandleFunc("POST /logout", s.postLogout)
	mux.HandleFunc("POST /locale", s.postLocale)

	mux.HandleFunc("GET /", s.requireSession(s.dashboard))
	mux.HandleFunc("GET /nodes", s.requireSession(s.nodesList))
	mux.HandleFunc("GET /nodes/{id}", s.requireSession(s.nodeDetail))
	mux.HandleFunc("GET /nodes/{id}/plan", s.requireSession(s.nodePlan))
	mux.HandleFunc("POST /nodes/{id}/dry-run", s.requireSession(s.nodeDryRun))
	mux.HandleFunc("POST /nodes/{id}/apply", s.requireSession(s.nodeApply))
	mux.HandleFunc("POST /nodes/{id}/failback", s.requireSession(s.nodeFailback))

	mux.HandleFunc("GET /backends", s.requireSession(s.backendsList))
	mux.HandleFunc("GET /backends/{id}", s.requireSession(s.backendDetail))
	mux.HandleFunc("POST /backends", s.requireSession(s.backendCreate))
	mux.HandleFunc("POST /backends/{id}", s.requireSession(s.backendUpdate))

	mux.HandleFunc("GET /tunnels", s.requireSession(s.tunnelsList))
	mux.HandleFunc("POST /tunnels", s.requireSession(s.tunnelCreate))

	mux.HandleFunc("GET /mappings", s.requireSession(s.mappingsList))
	mux.HandleFunc("POST /mappings", s.requireSession(s.mappingCreate))
	mux.HandleFunc("POST /mappings/{id}", s.requireSession(s.mappingUpdate))
	mux.HandleFunc("PATCH /mappings/{id}", s.requireSession(s.mappingPatch))
	mux.HandleFunc("POST /mappings/{id}/delete", s.requireSession(s.mappingDelete))

	mux.HandleFunc("GET /sni-routes", s.requireSession(s.sniList))
	mux.HandleFunc("GET /sni-routes/{id}", s.requireSession(s.sniDetail))
	mux.HandleFunc("POST /sni-routes", s.requireSession(s.sniCreate))
	mux.HandleFunc("POST /sni-routes/{id}", s.requireSession(s.sniUpdate))

	mux.HandleFunc("GET /events", s.requireSession(s.eventsList))
	mux.HandleFunc("GET /users", s.requireSession(s.requireAdmin(s.usersList)))
	mux.HandleFunc("POST /users", s.requireSession(s.requireAdmin(s.userCreate)))
	mux.HandleFunc("POST /users/{id}", s.requireSession(s.requireAdmin(s.userUpdate)))
	mux.HandleFunc("GET /settings", s.requireSession(s.settingsPage))
	mux.HandleFunc("POST /settings/password", s.requireSession(s.settingsPassword))
	mux.HandleFunc("POST /settings/totp/begin", s.requireSession(s.settingsTOTPBegin))
	mux.HandleFunc("POST /settings/totp/confirm", s.requireSession(s.settingsTOTPConfirm))
	mux.HandleFunc("POST /settings/totp/disable", s.requireSession(s.settingsTOTPDisable))
	mux.HandleFunc("POST /settings/recovery", s.requireSession(s.settingsRecovery))
	mux.HandleFunc("GET /api-reference", s.requireSession(s.apiReference))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) Start(ctx context.Context) error {
	if strings.TrimSpace(s.listen) == "" {
		return fmt.Errorf("webui: listen address is empty")
	}
	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	if !isLoopbackAddr(s.listen) && s.log != nil {
		s.log.Warn("web UI is not bound to loopback; treat it like the API and do not expose it publicly", logging.Fields{
			Event: logging.EventAudit,
			Extra: map[string]any{"listen": s.listen},
		})
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && ctx.Err() == nil && s.log != nil {
			s.log.Error("web UI server stopped", logging.Fields{Error: err.Error()})
		}
	}()
	if s.log != nil {
		s.log.Info("web UI listening", logging.Fields{Extra: map[string]any{"listen": s.listen}})
	}
	return nil
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) api(r *http.Request) API {
	token := ""
	var sess *session
	if sess = s.sessionFrom(r); sess != nil {
		token = sess.Token
	}
	if s.newClient != nil {
		return s.newClient(token)
	}
	rt := &loopback{h: s.apiHandler, auth: s.auth, sess: sess}
	return newLiveAPI(token, rt)
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.needsSetup() {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		sess := s.sessionFrom(r)
		if sess == nil {
			if hx(r) {
				w.Header().Set("HX-Redirect", "/login")
				http.Error(w, "session expired", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if sess.MFAPending {
			if hx(r) {
				w.Header().Set("HX-Redirect", "/login/mfa")
				http.Error(w, "mfa required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login/mfa", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.sessionFrom(r)
		if sess == nil || !canAdmin(sess.Role) {
			s.writeForbidden(w, r)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireWrite(w http.ResponseWriter, r *http.Request) bool {
	sess := s.sessionFrom(r)
	if sess == nil || !canWrite(sess.Role) {
		s.writeForbidden(w, r)
		return false
	}
	return true
}

func (s *Server) writeForbidden(w http.ResponseWriter, r *http.Request) {
	tr := s.T(r)
	if hx(r) {
		s.renderStatus(w, r, http.StatusForbidden, "alert", s.pageBase(r, "", "").withAlert(alertView{
			Kind: "forbidden", Title: tr("error.not_allowed_title"), Message: tr("error.not_allowed"),
		}))
		return
	}
	http.Error(w, tr("error.not_allowed"), http.StatusForbidden)
}

var pageCrumbs = map[string][2]string{
	"dashboard": {"nav.group.overview", "nav.dashboard"},
	"nodes":     {"nav.group.infrastructure", "nav.entry_nodes"},
	"backends":  {"nav.group.infrastructure", "nav.backends"},
	"tunnels":   {"nav.group.infrastructure", "nav.tunnels"},
	"mappings":  {"nav.group.infrastructure", "nav.mappings"},
	"sni":       {"nav.group.infrastructure", "nav.sni_routes"},
	"events":    {"nav.group.monitoring", "nav.events"},
	"users":     {"nav.group.administration", "nav.users"},
	"apiref":    {"nav.group.administration", "nav.api_reference"},
	"settings":  {"nav.group.administration", "nav.settings"},
}

func (s *Server) navCollapsed(r *http.Request) bool {
	c, err := r.Cookie(navCookie)
	return err == nil && c.Value == "collapsed"
}

func (s *Server) pageBase(r *http.Request, title, nav string) page {
	sess := s.sessionFrom(r)
	loc := s.locale(r)
	p := page{
		Title:        title,
		Nav:          nav,
		LiveApply:    s.liveApply,
		Locale:       loc,
		T:            i18n.Translator(loc),
		CanAdmin:     sess != nil && canAdmin(sess.Role),
		NavCollapsed: s.navCollapsed(r),
	}
	if c, ok := pageCrumbs[nav]; ok {
		p.CrumbGroup, p.CrumbPage = c[0], c[1]
	}
	if sess != nil {
		p.Principal = &model.PrincipalView{Name: sess.Name, Role: sess.Role}
		p.DisplayName = sess.DisplayName
		if p.DisplayName == "" {
			p.DisplayName = sess.Name
		}
		p.CanWrite = canWrite(sess.Role)
		p.UserID = string(sess.UserID)
		p.TokenSession = sess.Token != "" && sess.UserID == ""
		ok, errKey, raw := s.sessions.takeFlash(sess.ID)
		if ok != "" {
			p.FlashOK = i18n.T(loc, ok)
		}
		if errKey != "" {
			p.FlashErr = i18n.T(loc, errKey)
		}
		if raw != "" {
			p.FlashErr = raw
		}
	}
	return p
}

func (p page) withAlert(a alertView) page {
	if p.T != nil {
		if a.Title == "" && a.TitleKey != "" {
			a.Title = p.T(a.TitleKey)
		}
		if a.Message == "" && a.MessageKey != "" {
			a.Message = p.T(a.MessageKey)
		} else if a.Message == "" {
			a.Message = a.MessageKey
		}
	}
	p.Data = a
	return p
}

func (s *Server) flash(r *http.Request, ok, errMsg string) {
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.setFlash(sess.ID, ok, errMsg)
	}
}

func (s *Server) flashRaw(r *http.Request, errMsg string) {
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.setFlashRaw(sess.ID, errMsg)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, path string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", path)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}
