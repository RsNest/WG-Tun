package webui

import (
	"net/http"
	"strings"

	"proxyctl/internal/model"
)

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if s.sessionFrom(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", page{Title: "Sign in", Nav: "login", FlashErr: knownLoginErr(r.URL.Query().Get("err"))})
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?err=invalid+form", http.StatusSeeOther)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	if token == "" {
		http.Redirect(w, r, "/login?err=token+required", http.StatusSeeOther)
		return
	}
	api := s.newClient(token)
	me, err := api.Whoami(r.Context())
	if err != nil {
		http.Redirect(w, r, "/login?err=invalid+token", http.StatusSeeOther)
		return
	}
	if me.Role != model.RoleOperator && me.Role != model.RoleReadonly {
		http.Redirect(w, r, "/login?err=use+an+operator+or+readonly+token", http.StatusSeeOther)
		return
	}
	sess, err := s.sessions.put(token, me.Name, me.Role)
	if err != nil {
		http.Redirect(w, r, "/login?err=session+failed", http.StatusSeeOther)
		return
	}
	s.setSessionCookie(w, sess)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.delete(sess.ID)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func knownLoginErr(raw string) string {
	switch raw {
	case "invalid form", "token required", "invalid token", "use an operator or readonly token", "session failed":
		return raw
	default:
		return ""
	}
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	ctx := r.Context()
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.dashboardFetchErr(w, r, err)
		return
	}
	mappings, err := api.ListMappings(ctx)
	if err != nil {
		s.dashboardFetchErr(w, r, err)
		return
	}
	now := s.now()
	cards := make([]nodeCard, 0, len(nodes))
	for _, n := range nodes {
		runtime, _ := api.GetActualState(ctx, string(n.ID))
		cards = append(cards, buildNodeCard(now, n, mappings, runtime))
	}
	p := s.pageBase(r, "Dashboard", "dashboard")
	p.Data = cards
	p.Partial = hx(r)
	if p.Partial {
		s.render(w, r, "dashboard-cards", p)
		return
	}
	s.render(w, r, "dashboard", p)
}

func (s *Server) dashboardFetchErr(w http.ResponseWriter, r *http.Request, err error) {
	status, _ := classifyUIError(err)
	if status == http.StatusUnauthorized || !hx(r) {
		s.pageErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusBadGateway)
}

func (s *Server) nodesList(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	nodes, err := api.ListNodes(r.Context())
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	p := s.pageBase(r, "Nodes", "nodes")
	p.Data = nodes
	s.render(w, r, "nodes", p)
}

func (s *Server) nodeDetail(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	ctx := r.Context()
	id := r.PathValue("id")
	node, err := api.GetNode(ctx, id)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	ds, err := api.DesiredState(ctx, id)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	runtime, _ := api.GetActualState(ctx, id)
	planText := "NO CHANGES\n"
	if pv, err := api.Plan(ctx, id); err == nil && pv != nil && pv.Plan != "" {
		planText = pv.Plan
	} else if err != nil {
		s.pageErr(w, r, err)
		return
	}
	allMappings, err := api.ListMappings(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	var nodeMappings []model.PortMapping
	for _, m := range allMappings {
		if m.NodeID == node.ID {
			nodeMappings = append(nodeMappings, m)
		}
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	cat := newCatalog([]model.Node{*node}, backends)
	tunnels := make([]tunnelRow, 0, len(ds.Tunnels))
	for _, t := range ds.Tunnels {
		tunnels = append(tunnels, buildTunnelRow(t, cat, findTunnelActual(runtime, t)))
	}
	p := s.pageBase(r, node.Name, "nodes")
	p.Data = map[string]any{
		"Node":        node,
		"Card":        buildNodeCard(s.now(), *node, nodeMappings, runtime),
		"Tunnels":     tunnels,
		"Mappings":    nodeMappings,
		"Sni":         flattenSni(ds.SniRoutes, cat),
		"Plan":        buildPlanView(planText),
		"Failback":    failbackBackends(ds, runtime),
		"Catalog":     cat,
		"CanFailback": len(failbackBackends(ds, runtime)) > 0,
		"MgmtAddr":    managementAddr(*node),
		"Labels":      formatLabels(node.Labels),
	}
	s.render(w, r, "node_detail", p)
}

func (s *Server) backendsList(w http.ResponseWriter, r *http.Request) {
	s.renderBackends(w, r, emptyForm(), "")
}

func (s *Server) renderBackends(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, "Backends", "backends")
	p.Data = map[string]any{
		"Backends":  backends,
		"Nodes":     nodes,
		"Catalog":   newCatalog(nodes, backends),
		"Form":      form,
		"FormError": formErr,
	}
	s.render(w, r, "backends", p)
}

func (s *Server) backendDetail(w http.ResponseWriter, r *http.Request) {
	s.renderBackendDetail(w, r, r.PathValue("id"), emptyForm(), "")
}

func (s *Server) renderBackendDetail(w http.ResponseWriter, r *http.Request, id string, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	b, err := api.GetBackend(ctx, id)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	tunnels, err := api.ListTunnels(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	var related []model.Tunnel
	for _, t := range tunnels {
		if t.BackendID == b.ID {
			related = append(related, t)
		}
	}
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, b.Name, "backends")
	p.Data = map[string]any{
		"Backend":   b,
		"Nodes":     nodes,
		"Tunnels":   related,
		"Catalog":   newCatalog(nodes, []model.Backend{*b}),
		"Form":      form,
		"FormError": formErr,
	}
	s.render(w, r, "backend_detail", p)
}

func (s *Server) tunnelsList(w http.ResponseWriter, r *http.Request) {
	s.renderTunnels(w, r, emptyForm(), "")
}

func (s *Server) renderTunnels(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	tunnels, err := api.ListTunnels(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	cat := newCatalog(nodes, backends)
	runtime := map[model.ID]*model.NodeActualState{}
	rows := make([]tunnelRow, 0, len(tunnels))
	for _, t := range tunnels {
		if _, ok := runtime[t.NodeID]; !ok {
			runtime[t.NodeID], _ = api.GetActualState(ctx, string(t.NodeID))
		}
		rows = append(rows, buildTunnelRow(t, cat, findTunnelActual(runtime[t.NodeID], t)))
	}
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, "Tunnels", "tunnels")
	p.Data = map[string]any{
		"Rows":      rows,
		"Nodes":     nodes,
		"Backends":  backends,
		"Form":      form,
		"FormError": formErr,
	}
	s.render(w, r, "tunnels", p)
}

func (s *Server) mappingsList(w http.ResponseWriter, r *http.Request) {
	s.renderMappings(w, r, emptyForm(), "")
}

func (s *Server) renderMappings(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	mappings, err := api.ListMappings(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, "Mappings", "mappings")
	p.Data = map[string]any{
		"Mappings":  mappings,
		"Nodes":     nodes,
		"Backends":  backends,
		"Catalog":   newCatalog(nodes, backends),
		"Form":      form,
		"FormError": formErr,
	}
	if hx(r) {
		s.render(w, r, "mappings-table", p)
		return
	}
	s.render(w, r, "mappings", p)
}

func (s *Server) sniList(w http.ResponseWriter, r *http.Request) {
	s.renderSni(w, r, emptyForm(), "")
}

func (s *Server) renderSni(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	routes, err := api.ListSniRoutes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	cat := newCatalog(nodes, backends)
	if form == nil {
		form = emptyForm()
	}
	p := s.pageBase(r, "SNI routes", "sni")
	p.Data = map[string]any{
		"Rows":      flattenSni(routes, cat),
		"Nodes":     nodes,
		"Backends":  backends,
		"Form":      form,
		"FormError": formErr,
	}
	s.render(w, r, "sni", p)
}

func (s *Server) sniDetail(w http.ResponseWriter, r *http.Request) {
	s.renderSniDetail(w, r, r.PathValue("id"), emptyForm(), "")
}

func (s *Server) renderSniDetail(w http.ResponseWriter, r *http.Request, id string, form map[string]string, formErr string) {
	api := s.api(r)
	ctx := r.Context()
	route, err := api.GetSniRoute(ctx, id)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	cat := newCatalog(nodes, backends)
	def, extra := sniMatchesText(*route, cat)
	if form == nil {
		form = emptyForm()
	}
	if v := strings.TrimSpace(form["default_backend"]); v != "" {
		def = v
	}
	if _, ok := form["matches"]; ok {
		extra = form["matches"]
	}
	p := s.pageBase(r, "Edit SNI route", "sni")
	p.Data = map[string]any{
		"Route":          route,
		"Nodes":          nodes,
		"Backends":       backends,
		"DefaultBackend": def,
		"ExtraMatches":   extra,
		"Form":           form,
		"FormError":      formErr,
	}
	s.render(w, r, "sni_detail", p)
}

func (s *Server) eventsList(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	ctx := r.Context()
	events, err := api.ListEvents(ctx, queryString(r.URL))
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, _ := api.ListNodes(ctx)
	backends, _ := api.ListBackends(ctx)
	q := r.URL.Query()
	p := s.pageBase(r, "Events", "events")
	p.Data = map[string]any{
		"Events":   buildEventRows(events),
		"Nodes":    nodes,
		"Backends": backends,
		"Filter": map[string]string{
			"Node":    q.Get("node"),
			"Backend": q.Get("backend"),
			"Since":   q.Get("since"),
			"Until":   q.Get("until"),
			"Action":  q.Get("action"),
		},
		"FilterActive": q.Get("node") != "" || q.Get("backend") != "" || q.Get("since") != "" || q.Get("until") != "" || q.Get("action") != "",
	}
	s.render(w, r, "events", p)
}

func (s *Server) pageErr(w http.ResponseWriter, r *http.Request, err error) {
	status, a := classifyUIError(err)
	if status == http.StatusUnauthorized {
		if hx(r) {
			w.Header().Set("HX-Redirect", "/login")
			http.Error(w, "session expired", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login?err=invalid+token", http.StatusSeeOther)
		return
	}
	if hx(r) {
		s.renderStatus(w, r, status, "alert", page{Data: a})
		return
	}
	p := s.pageBase(r, a.Title, "")
	p.Data = a
	s.renderStatus(w, r, status, "error", p)
}
