package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"proxyctl/internal/auth"
	"proxyctl/internal/config"
	"proxyctl/internal/logging"
	"proxyctl/internal/model"
	"proxyctl/internal/store"
	"proxyctl/internal/version"
)

type Capabilities struct {
	LiveApply bool
	Failback  bool
	Metrics   http.Handler
}

type Server struct {
	cfg   *config.ControllerConfig
	store store.Store
	auth  *auth.Authenticator
	log   *logging.Logger
	cap   Capabilities
	ready bool
	mu    sync.RWMutex
	limit *limiter
	now   func() time.Time
}

func New(cfg *config.ControllerConfig, st store.Store, a *auth.Authenticator, log *logging.Logger, cap Capabilities) *Server {
	return &Server{
		cfg:   cfg,
		store: st,
		auth:  a,
		log:   log,
		cap:   cap,
		limit: newLimiter(cfg.RateLimit.MutatingRPS, cfg.RateLimit.Burst),
		now:   time.Now,
	}
}

func (s *Server) SetReady(v bool) {
	s.mu.Lock()
	s.ready = v
	s.mu.Unlock()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	if s.cap.Metrics != nil {
		mux.Handle("GET /metrics", s.cap.Metrics)
	} else {
		mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
			writeErr(w, model.NotImplemented("metrics not enabled yet"))
		})
	}

	mux.HandleFunc("GET /api/v1/whoami", s.authn(s.whoami, false))
	mux.HandleFunc("GET /api/v1/tokens", s.authn(s.listTokens, false))
	mux.HandleFunc("POST /api/v1/tokens", s.authn(s.createToken, true))

	mux.HandleFunc("GET /api/v1/nodes", s.authn(s.listNodes, false))
	mux.HandleFunc("POST /api/v1/nodes", s.authn(s.createNode, true))
	mux.HandleFunc("GET /api/v1/nodes/{id}", s.authn(s.getNode, false))
	mux.HandleFunc("GET /api/v1/nodes/{id}/desired-state", s.authn(s.desiredState, false))
	mux.HandleFunc("GET /api/v1/nodes/{id}/actual-state", s.authn(s.getActualState, false))
	mux.HandleFunc("POST /api/v1/nodes/{id}/actual-state", s.authn(s.putActualState, true))
	mux.HandleFunc("POST /api/v1/nodes/{id}/apply", s.authn(s.apply, true))
	mux.HandleFunc("POST /api/v1/nodes/{id}/failback", s.authn(s.failback, true))

	mux.HandleFunc("GET /api/v1/backends", s.authn(s.listBackends, false))
	mux.HandleFunc("POST /api/v1/backends", s.authn(s.createBackend, true))
	mux.HandleFunc("GET /api/v1/backends/{id}", s.authn(s.getBackend, false))
	mux.HandleFunc("PUT /api/v1/backends/{id}", s.authn(s.updateBackend, true))

	mux.HandleFunc("GET /api/v1/mappings", s.authn(s.listMappings, false))
	mux.HandleFunc("POST /api/v1/mappings", s.authn(s.createMapping, true))
	mux.HandleFunc("GET /api/v1/mappings/{id}", s.authn(s.getMapping, false))
	mux.HandleFunc("PUT /api/v1/mappings/{id}", s.authn(s.updateMapping, true))
	mux.HandleFunc("PATCH /api/v1/mappings/{id}", s.authn(s.patchMapping, true))
	mux.HandleFunc("DELETE /api/v1/mappings/{id}", s.authn(s.deleteMapping, true))

	mux.HandleFunc("GET /api/v1/tunnels", s.authn(s.listTunnels, false))
	mux.HandleFunc("POST /api/v1/tunnels", s.authn(s.createTunnel, true))
	mux.HandleFunc("GET /api/v1/tunnels/{id}/status", s.authn(s.tunnelStatus, false))

	mux.HandleFunc("GET /api/v1/sni-routes", s.authn(s.listSni, false))
	mux.HandleFunc("POST /api/v1/sni-routes", s.authn(s.createSni, true))
	mux.HandleFunc("GET /api/v1/sni-routes/{id}", s.authn(s.getSni, false))
	mux.HandleFunc("PUT /api/v1/sni-routes/{id}", s.authn(s.updateSni, true))

	mux.HandleFunc("GET /api/v1/audit", s.authn(s.listAudit, false))
	return mux
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	h := s.Handler()
	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
	}()
	if s.cfg.TLS.Required {
		if s.cfg.TLS.AutoSelfSigned {
			if err := config.EnsureSelfSigned(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile, s.cfg.Listen, s.cfg.TLS.DNSNames); err != nil {
				return err
			}
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		return srv.ServeTLS(ln, s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
	}
	s.log.Warn("controller listening without TLS", logging.Fields{Event: "audit", Extra: map[string]any{"listen": s.cfg.Listen}})
	return srv.Serve(ln)
}

type ctxKey int

const principalKey ctxKey = 1

func PrincipalFrom(ctx context.Context) *auth.Principal {
	p, _ := ctx.Value(principalKey).(*auth.Principal)
	return p
}

func (s *Server) authn(next http.HandlerFunc, mutate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeErr(w, model.Validation("could not read body"))
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(strings.NewReader(string(body)))

		if mutate && !s.limit.allow(clientIP(r)) {
			writeErr(w, &model.CodedError{Code: "RATE_LIMIT", Message: "too many mutating requests", HTTP: http.StatusTooManyRequests})
			return
		}
		p, err := s.auth.Authenticate(r, body)
		if err != nil {
			writeErr(w, err)
			return
		}
		if mutate && !auth.CanMutate(p.Role) {
			writeErr(w, model.Forbidden("role "+string(p.Role)+" cannot mutate"))
			return
		}
		if !mutate && !auth.CanRead(p.Role) {
			writeErr(w, model.Forbidden("role "+string(p.Role)+" cannot read"))
			return
		}
		ctx := context.WithValue(r.Context(), principalKey, p)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version.Version})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	ready := s.ready
	s.mu.RUnlock()
	if !ready {
		writeErr(w, model.Unavailable("controller not initialized"))
		return
	}
	if err := s.store.Ping(r.Context()); err != nil {
		writeErr(w, model.Unavailable("database not reachable"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	ce := model.AsCoded(err)
	if ce.HTTP == 0 {
		ce.HTTP = http.StatusInternalServerError
	}
	writeJSON(w, ce.HTTP, ce)
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return model.Validation("invalid json: " + err.Error())
	}
	return nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func actor(r *http.Request) string {
	if p := PrincipalFrom(r.Context()); p != nil {
		return p.Name
	}
	return "anonymous"
}
