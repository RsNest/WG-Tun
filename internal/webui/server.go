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

	"proxyctl/internal/logging"
	"proxyctl/internal/model"
)

// Config wires the optional operator UI.
type Config struct {
	Listen       string
	API          http.Handler
	NewClient    func(token string) API
	Log          *logging.Logger
	CookieSecure bool
	Now          func() time.Time
}

// Server is the localhost operator UI. It is not the data-plane API.
type Server struct {
	listen       string
	log          *logging.Logger
	newClient    func(token string) API
	sessions     *sessionStore
	tmpl         *template.Template
	static       fs.FS
	cookieSecure bool
	now          func() time.Time
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
	newClient := cfg.NewClient
	if newClient == nil {
		if cfg.API == nil {
			return nil, fmt.Errorf("webui: API handler or NewClient is required")
		}
		rt := &loopback{h: cfg.API}
		newClient = func(token string) API {
			return newLiveAPI(token, rt)
		}
	}
	return &Server{
		listen:       cfg.Listen,
		log:          cfg.Log,
		newClient:    newClient,
		sessions:     sessions,
		tmpl:         tmpl,
		static:       static,
		cookieSecure: cfg.CookieSecure,
		now:          cfg.Now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))))
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("POST /logout", s.postLogout)

	mux.HandleFunc("GET /", s.requireSession(s.dashboard))
	mux.HandleFunc("GET /nodes", s.requireSession(s.nodesList))
	mux.HandleFunc("GET /nodes/{id}", s.requireSession(s.nodeDetail))
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}

// Start binds the listen address and serves in a background goroutine.
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
	if sess := s.sessionFrom(r); sess != nil {
		token = sess.Token
	}
	return s.newClient(token)
}

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.sessionFrom(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	if r.Header.Get("HX-Request") == "true" {
		http.Error(w, "readonly session cannot mutate", http.StatusForbidden)
		return
	}
	http.Error(w, "forbidden: operator role required", http.StatusForbidden)
}

func (s *Server) pageBase(r *http.Request, title, nav string) page {
	sess := s.sessionFrom(r)
	p := page{Title: title, Nav: nav}
	if sess != nil {
		p.Principal = &model.PrincipalView{Name: sess.Name, Role: sess.Role}
		p.CanWrite = canWrite(sess.Role)
		p.FlashOK, p.FlashErr = s.sessions.takeFlash(sess.ID)
	}
	return p
}

func (s *Server) flash(r *http.Request, ok, errMsg string) {
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.setFlash(sess.ID, ok, errMsg)
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
