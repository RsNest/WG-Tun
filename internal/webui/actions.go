package webui

import (
	"net/http"
	"strconv"
	"strings"

	"proxyctl/internal/model"
)

func (s *Server) nodePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.api(r).Plan(r.Context(), id)
	if err != nil {
		if hx(r) {
			writePlain(w, http.StatusBadRequest, s.apiErr(err))
			return
		}
		s.flash(r, "", s.apiErr(err))
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	plan := "NO CHANGES\n"
	if res != nil && res.Plan != "" {
		plan = res.Plan
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	p := s.pageBase(r, "", "nodes")
	p.Data = plan
	s.render(w, r, "plan", p)
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
		if hx(r) {
			writePlain(w, http.StatusBadRequest, s.apiErr(err))
			return
		}
		s.flash(r, "", s.apiErr(err))
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	plan := res.Plan
	if plan == "" {
		plan = res.Message
	}
	if hx(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		p := s.pageBase(r, "", "nodes")
		p.Data = plan
		s.render(w, r, "plan", p)
		return
	}
	s.flash(r, "plan refreshed", "")
	s.redirect(w, r, "/nodes/"+id)
}

func (s *Server) nodeFailback(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, "invalid form")
		return
	}
	id := r.PathValue("id")
	backend := firstNonEmpty(r.FormValue("backend"), r.FormValue("backend_id"))
	if backend == "" {
		s.flash(r, "", "backend is required")
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	if err := s.api(r).Failback(r.Context(), id, backend); err != nil {
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/nodes/"+id)
		return
	}
	s.flash(r, "failback requested", "")
	s.redirect(w, r, "/nodes/"+id)
}

func (s *Server) backendCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/backends")
		return
	}
	b := model.Backend{
		Name:    r.FormValue("name"),
		NodeID:  model.ID(r.FormValue("node_id")),
		Address: r.FormValue("address"),
	}
	out, err := s.api(r).CreateBackend(r.Context(), b)
	if err != nil {
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/backends")
		return
	}
	s.flash(r, "backend created", "")
	s.redirect(w, r, "/backends/"+string(out.ID))
}

func (s *Server) backendUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/backends/"+r.PathValue("id"))
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/backends/"+id)
		return
	}
	s.flash(r, "backend updated", "")
	s.redirect(w, r, "/backends/"+id)
}

func (s *Server) tunnelCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/tunnels")
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/tunnels")
		return
	}
	s.flash(r, "tunnel created", "")
	s.redirect(w, r, "/tunnels")
}

func (s *Server) mappingCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/mappings")
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/mappings")
		return
	}
	s.flash(r, "mapping created", "")
	s.redirect(w, r, "/mappings")
}

func (s *Server) mappingUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/mappings")
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/mappings")
		return
	}
	s.flash(r, "mapping updated", "")
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
		writePlain(w, http.StatusBadRequest, "enabled is required")
		return
	}
	enabled := raw == "true" || raw == "1" || raw == "on"
	if _, err := s.api(r).PatchMapping(r.Context(), id, model.MappingPatch{Enabled: &enabled}); err != nil {
		if hx(r) {
			writePlain(w, http.StatusBadRequest, err.Error())
			return
		}
		s.flash(r, "", err.Error())
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/mappings")
		return
	}
	s.flash(r, "mapping deleted", "")
	s.redirect(w, r, "/mappings")
}

func (s *Server) sniCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/sni-routes")
		return
	}
	route := model.SniRoute{
		NodeID:  model.ID(r.FormValue("node_id")),
		Listen:  firstNonEmpty(r.FormValue("listen"), ":443"),
		Matches: parseSniMatches(r.FormValue("default_backend"), r.FormValue("matches")),
	}
	if _, err := s.api(r).CreateSniRoute(r.Context(), route); err != nil {
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/sni-routes")
		return
	}
	s.flash(r, "SNI route created", "")
	s.redirect(w, r, "/sni-routes")
}

func (s *Server) sniUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.flash(r, "", "invalid form")
		s.redirect(w, r, "/sni-routes/"+r.PathValue("id"))
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
		s.flash(r, "", err.Error())
		s.redirect(w, r, "/sni-routes/"+id)
		return
	}
	s.flash(r, "SNI route updated", "")
	s.redirect(w, r, "/sni-routes")
}

func (s *Server) apiErr(err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "token") || strings.Contains(low, "bearer") || strings.Contains(low, "private") {
		return "request failed"
	}
	return msg
}
