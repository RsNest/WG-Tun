package webui

import (
	"net/http"
	"strconv"
	"strings"

	"transitforge/internal/model"
	"transitforge/internal/webui/i18n"
)

func (s *Server) nodePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.api(r).Plan(r.Context(), id)
	if err != nil {
		if hx(r) {
			s.writePlanFragment(w, r, planViewFromError(err))
			return
		}
		s.pageErr(w, r, err)
		return
	}
	plan := ""
	if res != nil {
		plan = res.Plan
	}
	s.writePlanFragment(w, r, buildPlanView(plan))
}

func (s *Server) nodeDryRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	id := r.PathValue("id")
	res, err := s.api(r).Apply(r.Context(), id, true)
	if err != nil {
		s.writePlan(w, r, id, planViewFromError(err), err)
		return
	}
	view := planViewFromApply(res)
	view.Notice = i18n.T(s.locale(r), "flash.dry_run_recorded")
	s.writePlan(w, r, id, view, nil)
}

func (s *Server) nodeApply(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	id := r.PathValue("id")
	if !s.liveApply {
		writePlain(w, http.StatusNotImplemented, "Live apply is not enabled on this controller.")
		return
	}
	res, err := s.api(r).Apply(r.Context(), id, false)
	if err != nil {
		s.writePlan(w, r, id, planViewFromError(err), err)
		return
	}
	view := planViewFromApply(res)
	if view.Notice == "" && view.Error == "" {
		view.Notice = i18n.T(s.locale(r), "flash.apply_completed")
	}
	s.writePlan(w, r, id, view, nil)
}

func (s *Server) writePlan(w http.ResponseWriter, r *http.Request, id string, view planView, err error) {
	if !hx(r) {
		if err != nil {
			s.flashRaw(r, firstNonEmpty(view.Error, s.apiErr(err)))
		} else if view.Notice != "" {
			s.flash(r, view.Notice, "")
		}
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	s.writePlanFragment(w, r, view)
}

func (s *Server) writePlanFragment(w http.ResponseWriter, r *http.Request, view planView) {
	p := s.pageBase(r, "", "nodes")
	p.Data = view
	s.render(w, r, "plan", p)
}

func planViewFromApply(res *model.ApplyResult) planView {
	if res == nil {
		return planView{NoChanges: true}
	}
	raw := res.Plan
	if strings.TrimSpace(raw) == "" {
		raw = res.Message
	}
	return buildPlanView(raw)
}

func (s *Server) nodeFailback(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "flash.invalid_form")
		s.redirect(w, r, "/nodes/"+r.PathValue("id"))
		return
	}
	id := r.PathValue("id")
	backend := firstNonEmpty(r.FormValue("backend"), r.FormValue("backend_id"))
	if backend == "" {
		s.flash(r, "", "flash.backend_required")
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	if err := s.api(r).Failback(r.Context(), id, backend); err != nil {
		s.flashRaw(r, s.apiErr(err))
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	s.flash(r, "flash.failback", "")
	s.redirect(w, r, "/nodes/"+id)
}

func (s *Server) backendCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderBackends(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	b := model.Backend{
		Name:    r.FormValue("name"),
		NodeID:  model.ID(r.FormValue("node_id")),
		Address: r.FormValue("address"),
	}
	out, err := s.api(r).CreateBackend(r.Context(), b)
	if err != nil {
		s.renderBackends(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.backend_created", "")
	s.redirect(w, r, "/backends/"+string(out.ID))
}

func (s *Server) backendUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderBackendDetail(w, r, r.PathValue("id"), emptyForm(), "flash.invalid_form")
		return
	}
	id := r.PathValue("id")
	b := model.Backend{
		ID:      model.ID(id),
		Name:    r.FormValue("name"),
		NodeID:  model.ID(r.FormValue("node_id")),
		Address: r.FormValue("address"),
	}
	if _, err := s.api(r).PatchBackend(r.Context(), b); err != nil {
		s.renderBackendDetail(w, r, id, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.backend_updated", "")
	s.redirect(w, r, "/backends/"+id)
}

func (s *Server) tunnelCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderTunnels(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	listen, _ := strconv.Atoi(r.FormValue("listen_port"))
	ka, _ := strconv.Atoi(r.FormValue("persistent_keepalive"))
	prio, _ := strconv.Atoi(r.FormValue("priority"))
	t := model.Tunnel{
		NodeID:              model.ID(r.FormValue("node_id")),
		BackendID:           model.ID(r.FormValue("backend_id")),
		Type:                model.TunnelWireGuard,
		InterfaceName:       r.FormValue("interface_name"),
		LocalOverlayIP:      r.FormValue("local_overlay_ip"),
		RemoteOverlayIP:     r.FormValue("remote_overlay_ip"),
		ListenPort:          listen,
		Endpoint:            r.FormValue("endpoint"),
		AllowedIPs:          parseAllowedIPs(r.FormValue("allowed_ips")),
		PersistentKeepalive: ka,
		Priority:            prio,
		PrivateKeyPath:      r.FormValue("private_key_path"),
	}
	if _, err := s.api(r).CreateTunnel(r.Context(), t); err != nil {
		s.renderTunnels(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.tunnel_created", "")
	s.redirect(w, r, "/tunnels")
}

func (s *Server) mappingCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderMappings(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	pub, _ := strconv.Atoi(r.FormValue("public_port"))
	bp, _ := strconv.Atoi(r.FormValue("backend_port"))
	m := model.PortMapping{
		NodeID:      model.ID(r.FormValue("node_id")),
		BackendID:   model.ID(r.FormValue("backend_id")),
		Protocol:    model.Protocol(r.FormValue("protocol")),
		PublicPort:  pub,
		BackendPort: bp,
	}
	if _, err := s.api(r).CreateMapping(r.Context(), m); err != nil {
		s.renderMappings(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.mapping_created", "")
	s.redirect(w, r, "/mappings")
}

func (s *Server) mappingUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderMappings(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	id := r.PathValue("id")
	pub, _ := strconv.Atoi(r.FormValue("public_port"))
	bp, _ := strconv.Atoi(r.FormValue("backend_port"))
	enabled := r.FormValue("enabled") != "false" && r.FormValue("enabled") != "0"
	nodeID := model.ID(r.FormValue("node_id"))
	backendID := model.ID(r.FormValue("backend_id"))
	proto := model.Protocol(r.FormValue("protocol"))
	patch := model.MappingPatch{
		Enabled:     &enabled,
		PublicPort:  &pub,
		BackendPort: &bp,
	}
	if nodeID != "" {
		patch.NodeID = &nodeID
	}
	if backendID != "" {
		patch.BackendID = &backendID
	}
	if proto != "" {
		patch.Protocol = &proto
	}
	if _, err := s.api(r).PatchMapping(r.Context(), id, patch); err != nil {
		s.renderMappings(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.mapping_updated", "")
	s.redirect(w, r, "/mappings")
}

func (s *Server) mappingPatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	_ = r.ParseForm()
	id := r.PathValue("id")
	raw := firstNonEmpty(r.FormValue("enabled"), r.URL.Query().Get("enabled"))
	if raw == "" {
		if hx(r) {
			s.renderStatus(w, r, http.StatusBadRequest, "alert", s.pageBase(r, "", "").withAlert(alertView{
				Kind: "validation", TitleKey: "error.invalid_title", MessageKey: "error.enabled_required",
			}))
			return
		}
		s.flash(r, "", "error.enabled_required")
		s.redirect(w, r, "/mappings")
		return
	}
	enabled := raw == "true" || raw == "1" || raw == "on"
	if _, err := s.api(r).PatchMapping(r.Context(), id, model.MappingPatch{Enabled: &enabled}); err != nil {
		if hx(r) {
			status, a := classifyUIError(err)
			s.renderStatus(w, r, status, "alert", s.pageBase(r, "", "").withAlert(a))
			return
		}
		s.flashRaw(r, s.apiErr(err))
		s.redirect(w, r, "/mappings")
		return
	}
	if hx(r) {
		s.mappingsList(w, r)
		return
	}
	s.redirect(w, r, "/mappings")
}

func (s *Server) mappingDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.api(r).DeleteMapping(r.Context(), id); err != nil {
		s.flashRaw(r, s.apiErr(err))
		s.redirect(w, r, "/mappings")
		return
	}
	s.flash(r, "flash.mapping_deleted", "")
	s.redirect(w, r, "/mappings")
}

func (s *Server) sniCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderSni(w, r, emptyForm(), "flash.invalid_form")
		return
	}
	route := model.SniRoute{
		NodeID:  model.ID(r.FormValue("node_id")),
		Listen:  firstNonEmpty(r.FormValue("listen"), ":443"),
		Matches: parseSniMatches(r.FormValue("default_backend"), r.FormValue("matches")),
	}
	if _, err := s.api(r).CreateSniRoute(r.Context(), route); err != nil {
		s.renderSni(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.sni_created", "")
	s.redirect(w, r, "/sni-routes")
}

func (s *Server) sniUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderSniDetail(w, r, r.PathValue("id"), emptyForm(), "flash.invalid_form")
		return
	}
	id := r.PathValue("id")
	route := model.SniRoute{
		ID:      model.ID(id),
		NodeID:  model.ID(r.FormValue("node_id")),
		Listen:  firstNonEmpty(r.FormValue("listen"), ":443"),
		Matches: parseSniMatches(r.FormValue("default_backend"), r.FormValue("matches")),
	}
	if _, err := s.api(r).PatchSniRoute(r.Context(), route); err != nil {
		s.renderSniDetail(w, r, id, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "flash.sni_updated", "")
	s.redirect(w, r, "/sni-routes")
}

func (s *Server) apiErr(err error) string {
	_, a := classifyUIError(err)
	return a.resolve("en").Message
}

func (s *Server) apiErrT(r *http.Request, err error) string {
	_, a := classifyUIError(err)
	return s.localizeAlert(r, a).Message
}

func formMap(r *http.Request) map[string]string {
	out := emptyForm()
	if r.PostForm == nil {
		return out
	}
	for k, vs := range r.PostForm {
		if skipFormKey(k) {
			continue
		}
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func skipFormKey(k string) bool {
	kl := strings.ToLower(strings.TrimSpace(k))
	switch {
	case kl == "token", kl == "password", strings.Contains(kl, "secret"):
		return true
	case strings.Contains(kl, "private") && !strings.Contains(kl, "path"):
		return true
	default:
		return false
	}
}
