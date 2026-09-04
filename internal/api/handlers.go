package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"proxyctl/internal/ident"
	"proxyctl/internal/logging"
	"proxyctl/internal/model"
	"proxyctl/internal/reconcile"
	"proxyctl/internal/store"
)

func (s *Server) audit(r *http.Request, action, resource, id, detail string, success bool) {
	_ = s.store.AppendAudit(r.Context(), &model.AuditEvent{
		ID:         ident.New(),
		Timestamp:  time.Now().UTC(),
		Actor:      actor(r),
		Action:     action,
		Resource:   resource,
		ResourceID: id,
		Detail:     logging.Redact(detail),
		Success:    success,
	})
}

func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p == nil {
		writeErr(w, model.Unauthorized("no principal"))
		return
	}
	writeJSON(w, http.StatusOK, model.PrincipalView{Name: p.Name, Role: p.Role})
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p == nil || p.Role != model.RoleOperator {
		writeErr(w, model.Forbidden("only operator can list tokens"))
		return
	}
	out, err := s.store.ListTokens(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p == nil || p.Role != model.RoleOperator {
		writeErr(w, model.Forbidden("only operator can mint tokens"))
		return
	}
	var req model.TokenCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	role, err := model.ParseRole(string(req.Role))
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, model.Validation("name is required"))
		return
	}
	plain, tok, err := s.auth.CreateToken(r.Context(), strings.TrimSpace(req.Name), role)
	if err != nil {
		s.audit(r, "create", "token", "", err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "token", string(tok.ID), string(role), true)
	writeJSON(w, http.StatusCreated, model.TokenCreateResult{
		ID:        tok.ID,
		Name:      tok.Name,
		Role:      tok.Role,
		Token:     plain,
		CreatedAt: tok.CreatedAt,
	})
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var n model.Node
	if err := decodeJSON(r, &n); err != nil {
		writeErr(w, err)
		return
	}
	if err := n.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	n.ID = ident.New()
	n.CreatedAt = time.Now().UTC()
	n.UpdatedAt = n.CreatedAt
	if err := s.store.CreateNode(r.Context(), &n); err != nil {
		s.audit(r, "create", "node", string(n.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "node", string(n.ID), n.Name, true)
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListNodes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = []model.Node{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	n, err := s.store.GetNode(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("node", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) createBackend(w http.ResponseWriter, r *http.Request) {
	var b model.Backend
	if err := decodeJSON(r, &b); err != nil {
		writeErr(w, err)
		return
	}
	if b.NodeID == "" {
		if name := r.URL.Query().Get("node"); name != "" {
			n, err := s.store.GetNodeByName(r.Context(), name)
			if err != nil {
				writeErr(w, model.NotFound("node", name))
				return
			}
			b.NodeID = n.ID
		}
	}
	if err := b.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.store.GetNode(r.Context(), b.NodeID); err != nil {
		writeErr(w, model.NotFound("node", string(b.NodeID)))
		return
	}
	b.ID = ident.New()
	b.CreatedAt = time.Now().UTC()
	b.UpdatedAt = b.CreatedAt
	if err := s.store.CreateBackend(r.Context(), &b); err != nil {
		s.audit(r, "create", "backend", string(b.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "backend", string(b.ID), b.Name, true)
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) listBackends(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListBackends(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = []model.Backend{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createMapping(w http.ResponseWriter, r *http.Request) {
	var m model.PortMapping
	if err := decodeJSON(r, &m); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.resolveMappingRefs(r, &m); err != nil {
		writeErr(w, err)
		return
	}
	if err := m.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	m.ID = ident.New()
	m.CreatedAt = time.Now().UTC()
	m.UpdatedAt = m.CreatedAt
	if err := s.store.CreateMapping(r.Context(), &m); err != nil {
		s.audit(r, "create", "mapping", string(m.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "mapping", string(m.ID), "", true)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) resolveMappingRefs(r *http.Request, m *model.PortMapping) error {
	if m.NodeID == "" {
		if name := r.URL.Query().Get("node"); name != "" {
			n, err := s.store.GetNodeByName(r.Context(), name)
			if err != nil {
				return model.NotFound("node", name)
			}
			m.NodeID = n.ID
		}
	}
	if m.BackendID == "" {
		if name := r.URL.Query().Get("backend"); name != "" {
			b, err := s.store.GetBackendByName(r.Context(), name)
			if err != nil {
				return model.NotFound("backend", name)
			}
			m.BackendID = b.ID
			if m.NodeID == "" {
				m.NodeID = b.NodeID
			}
		}
	}
	return nil
}

func (s *Server) listMappings(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListMappings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = []model.PortMapping{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteMapping(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if err := s.store.DeleteMapping(r.Context(), id); err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("mapping", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	s.audit(r, "delete", "mapping", string(id), "", true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createTunnel(w http.ResponseWriter, r *http.Request) {
	var t model.Tunnel
	if err := decodeJSON(r, &t); err != nil {
		writeErr(w, err)
		return
	}
	if err := t.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.store.GetNode(r.Context(), t.NodeID); err != nil {
		writeErr(w, model.NotFound("node", string(t.NodeID)))
		return
	}
	if _, err := s.store.GetBackend(r.Context(), t.BackendID); err != nil {
		writeErr(w, model.NotFound("backend", string(t.BackendID)))
		return
	}
	t.ID = ident.New()
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	if err := s.store.CreateTunnel(r.Context(), &t); err != nil {
		s.audit(r, "create", "tunnel", string(t.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "tunnel", string(t.ID), string(t.Type), true)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) listTunnels(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListTunnels(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = []model.Tunnel{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) tunnelStatus(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	t, err := s.store.GetTunnel(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("tunnel", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	st := model.TunnelStatus{Tunnel: *t, Healthy: false, Detail: "no actual-state reported yet"}
	actual, _, err := s.store.GetActualState(r.Context(), t.NodeID)
	if err == nil && actual != nil {
		for i := range actual.Tunnels {
			a := actual.Tunnels[i]
			if a.TunnelID == t.ID || a.InterfaceName == t.InterfaceName {
				cp := a
				st.Actual = &cp
				st.Healthy = a.InterfacePresent && (t.Type != model.TunnelWireGuard || a.HandshakeAgeSec < 180)
				if st.Healthy {
					st.Detail = "interface present"
				} else {
					st.Detail = "interface missing or handshake stale"
				}
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) createSni(w http.ResponseWriter, r *http.Request) {
	var route model.SniRoute
	if err := decodeJSON(r, &route); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.resolveSniBackends(r, &route); err != nil {
		writeErr(w, err)
		return
	}
	if err := route.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	route.ID = ident.New()
	route.CreatedAt = time.Now().UTC()
	route.UpdatedAt = route.CreatedAt
	if err := s.store.CreateSniRoute(r.Context(), &route); err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "sni_route", string(route.ID), route.Listen, true)
	writeJSON(w, http.StatusCreated, route)
}

func (s *Server) resolveSniBackends(r *http.Request, route *model.SniRoute) error {
	for i := range route.Matches {
		m := &route.Matches[i]
		if m.BackendID != "" {
			continue
		}
		if m.Backend == "" {
			continue
		}
		b, err := s.store.GetBackendByName(r.Context(), m.Backend)
		if err != nil {
			return model.NotFound("backend", m.Backend)
		}
		m.BackendID = b.ID
	}
	return nil
}

func (s *Server) desiredState(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	ds, err := s.store.DesiredState(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("node", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (s *Server) putActualState(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.store.GetNode(r.Context(), id); err != nil {
		writeErr(w, model.NotFound("node", string(id)))
		return
	}
	var st model.ActualState
	if err := decodeJSON(r, &st); err != nil {
		writeErr(w, err)
		return
	}
	st.NodeID = id
	if st.DiscoveredAt.IsZero() {
		st.DiscoveredAt = time.Now().UTC()
	}
	status := model.AgentStatus{
		NodeID:        id,
		Healthy:       len(st.Conflicts) == 0,
		LastHeartbeat: time.Now().UTC(),
		LastReconcile: st.DiscoveredAt,
		Version:       r.Header.Get("X-Proxyctl-Agent-Version"),
	}
	if err := s.store.PutActualState(r.Context(), id, st, status); err != nil {
		writeErr(w, err)
		return
	}
	s.log.Info("actual-state received", logging.Fields{Event: logging.EventAgentRegistered, Extra: map[string]any{"node_id": string(id)}})
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "recorded"})
}

func (s *Server) computePlan(ctx context.Context, id model.ID) (reconcile.Plan, error) {
	ds, err := s.store.DesiredState(ctx, id)
	if err != nil {
		return reconcile.Plan{}, err
	}
	actual := model.ActualState{NodeID: id}
	if st, _, err := s.store.GetActualState(ctx, id); err == nil && st != nil {
		actual = *st
	}
	return reconcile.Diff(*ds, actual), nil
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	plan, err := s.computePlan(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("node", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.PlanView{Plan: plan.String()})
}

func (s *Server) apply(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	var req model.ApplyRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, err)
			return
		}
	}
	plan, err := s.computePlan(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("node", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	if plan.HasConflicts() {
		s.audit(r, "apply", "node", string(id), plan.String(), false)
		writeErr(w, model.ErrConflict(plan.String()))
		return
	}
	if req.DryRun || !s.cap.LiveApply {
		msg := ""
		if !req.DryRun && !s.cap.LiveApply {
			msg = "live apply is not enabled on this controller; returning dry-run plan only"
		}
		if plan.Empty() {
			msg = "NO CHANGES"
		}
		res := model.ApplyResult{DryRun: true, Applied: false, Plan: plan.String(), Message: msg}
		s.audit(r, "apply-dry-run", "node", string(id), plan.String(), true)
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeErr(w, model.NotImplemented("controller-side live apply is agent-driven; use the edge agent"))
}

func (s *Server) failback(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.store.GetNode(r.Context(), id); err != nil {
		writeErr(w, model.NotFound("node", string(id)))
		return
	}
	if !s.cap.Failback {
		writeErr(w, model.NotImplemented("failback state machine is not enabled yet"))
		return
	}
	var req struct {
		BackendID string `json:"backend_id"`
		Backend   string `json:"backend"`
		Action    string `json:"action"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, err)
			return
		}
	}
	if req.Action == "" {
		req.Action = "fail_forward"
	}
	if req.Action != "fail_forward" {
		writeErr(w, model.Validation("action must be fail_forward"))
		return
	}
	var bid model.ID
	if req.BackendID != "" {
		bid = model.ID(req.BackendID)
	} else if req.Backend != "" {
		b, err := s.store.GetBackendByName(r.Context(), req.Backend)
		if err != nil {
			writeErr(w, model.NotFound("backend", req.Backend))
			return
		}
		bid = b.ID
	} else {
		writeErr(w, model.Validation("backend or backend_id is required"))
		return
	}
	if _, err := s.store.GetBackend(r.Context(), bid); err != nil {
		writeErr(w, model.NotFound("backend", string(bid)))
		return
	}
	in := &model.FailbackIntent{
		ID:        ident.New(),
		NodeID:    id,
		BackendID: bid,
		Action:    req.Action,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateFailbackIntent(r.Context(), in); err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "failback", "node", string(id), req.Action, true)
	writeJSON(w, http.StatusAccepted, in)
}

func (s *Server) getBackend(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	b, err := s.store.GetBackend(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("backend", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) patchBackend(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	existing, err := s.store.GetBackend(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("backend", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	var b model.Backend
	if err := decodeJSON(r, &b); err != nil {
		writeErr(w, err)
		return
	}
	b.ID = existing.ID
	b.CreatedAt = existing.CreatedAt
	if b.Name == "" {
		b.Name = existing.Name
	}
	if b.NodeID == "" {
		b.NodeID = existing.NodeID
	}
	if b.Address == "" {
		b.Address = existing.Address
	}
	if err := b.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.store.GetNode(r.Context(), b.NodeID); err != nil {
		writeErr(w, model.NotFound("node", string(b.NodeID)))
		return
	}
	b.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateBackend(r.Context(), &b); err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("backend", string(id)))
			return
		}
		s.audit(r, "update", "backend", string(b.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "update", "backend", string(b.ID), b.Name, true)
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) patchMapping(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	existing, err := s.store.GetMapping(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("mapping", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	var patch model.MappingPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeErr(w, err)
		return
	}
	if patch.Empty() {
		writeErr(w, model.Validation("at least one mapping field is required"))
		return
	}
	m := *existing
	if patch.NodeID != nil && *patch.NodeID != "" {
		m.NodeID = *patch.NodeID
	}
	if patch.BackendID != nil && *patch.BackendID != "" {
		m.BackendID = *patch.BackendID
	}
	if patch.Backend != nil && strings.TrimSpace(*patch.Backend) != "" {
		b, err := s.store.GetBackendByName(r.Context(), strings.ToLower(strings.TrimSpace(*patch.Backend)))
		if err != nil {
			writeErr(w, model.NotFound("backend", strings.TrimSpace(*patch.Backend)))
			return
		}
		m.BackendID = b.ID
		if m.NodeID == "" {
			m.NodeID = b.NodeID
		}
	}
	if patch.Protocol != nil {
		m.Protocol = *patch.Protocol
	}
	if patch.PublicPort != nil {
		m.PublicPort = *patch.PublicPort
	}
	if patch.BackendPort != nil {
		m.BackendPort = *patch.BackendPort
	}
	if patch.Enabled != nil {
		m.Enabled = *patch.Enabled
	}
	if err := s.resolveMappingRefs(r, &m); err != nil {
		writeErr(w, err)
		return
	}
	if err := m.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	m.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateMapping(r.Context(), &m); err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("mapping", string(id)))
			return
		}
		s.audit(r, "update", "mapping", string(m.ID), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "update", "mapping", string(m.ID), "", true)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) listSni(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListSniRoutes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if out == nil {
		out = []model.SniRoute{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getSni(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	route, err := s.store.GetSniRoute(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("sni_route", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, route)
}

func (s *Server) patchSni(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	existing, err := s.store.GetSniRoute(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeErr(w, model.NotFound("sni_route", string(id)))
			return
		}
		writeErr(w, err)
		return
	}
	var route model.SniRoute
	if err := decodeJSON(r, &route); err != nil {
		writeErr(w, err)
		return
	}
	route.ID = existing.ID
	route.CreatedAt = existing.CreatedAt
	if route.NodeID == "" {
		route.NodeID = existing.NodeID
	}
	if route.Listen == "" {
		route.Listen = existing.Listen
	}
	if route.Matches == nil {
		route.Matches = existing.Matches
	}
	if err := s.resolveSniBackends(r, &route); err != nil {
		writeErr(w, err)
		return
	}
	if err := route.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	route.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateSniRoute(r.Context(), &route); err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "update", "sni_route", string(route.ID), route.Listen, true)
	writeJSON(w, http.StatusOK, route)
}

func (s *Server) getActualState(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.store.GetNode(r.Context(), id); err != nil {
		writeErr(w, model.NotFound("node", string(id)))
		return
	}
	st, status, err := s.store.GetActualState(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			writeJSON(w, http.StatusOK, model.NodeActualState{})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.NodeActualState{Actual: st, Status: status})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListAudit(r.Context(), 500)
	if err != nil {
		writeErr(w, err)
		return
	}
	if events == nil {
		events = []model.AuditEvent{}
	}
	filtered, err := s.filterEvents(r, events)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) filterEvents(r *http.Request, events []model.AuditEvent) ([]model.AuditEvent, error) {
	q := r.URL.Query()
	ids := map[string]bool{}
	if nodeQ := strings.TrimSpace(q.Get("node")); nodeQ != "" {
		n, err := s.store.GetNode(r.Context(), model.ID(nodeQ))
		if err != nil {
			n, err = s.store.GetNodeByName(r.Context(), strings.ToLower(nodeQ))
		}
		if err != nil {
			return []model.AuditEvent{}, nil
		}
		ids[string(n.ID)] = true
		backends, _ := s.store.ListBackendsByNode(r.Context(), n.ID)
		for _, b := range backends {
			ids[string(b.ID)] = true
		}
		tunnels, _ := s.store.ListTunnelsByNode(r.Context(), n.ID)
		for _, t := range tunnels {
			ids[string(t.ID)] = true
		}
		mappings, _ := s.store.ListMappingsByNode(r.Context(), n.ID)
		for _, m := range mappings {
			ids[string(m.ID)] = true
		}
		sni, _ := s.store.ListSniRoutesByNode(r.Context(), n.ID)
		for _, route := range sni {
			ids[string(route.ID)] = true
		}
	}
	if backendQ := strings.TrimSpace(q.Get("backend")); backendQ != "" {
		b, err := s.store.GetBackend(r.Context(), model.ID(backendQ))
		if err != nil {
			b, err = s.store.GetBackendByName(r.Context(), strings.ToLower(backendQ))
		}
		if err != nil {
			return []model.AuditEvent{}, nil
		}
		if len(ids) == 0 {
			ids[string(b.ID)] = true
			ids[string(b.NodeID)] = true
		} else {
			ids[string(b.ID)] = true
		}
	}
	since, err := parseEventTime(q.Get("since"), false)
	if err != nil {
		return nil, err
	}
	until, err := parseEventTime(q.Get("until"), true)
	if err != nil {
		return nil, err
	}
	action := strings.TrimSpace(q.Get("action"))
	out := make([]model.AuditEvent, 0, len(events))
	for _, e := range events {
		if len(ids) > 0 && !ids[e.ResourceID] {
			continue
		}
		if action != "" && e.Action != action {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && !e.Timestamp.Before(until) && !e.Timestamp.Equal(until) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func parseEventTime(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.UTC()
		if endOfDay {
			return t.Add(24 * time.Hour), nil
		}
		return t, nil
	}
	return time.Time{}, model.Validation("since/until must be YYYY-MM-DD or RFC3339")
}
