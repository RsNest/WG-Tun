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
		s.pageErr(w, r, err)
		return
	}
	mappings, err := api.ListMappings(ctx)
	if err != nil {
		s.pageErr(w, r, err)
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
		"Plan":        planText,
		"Failback":    failbackBackends(ds, runtime),
		"Catalog":     cat,
		"CanFailback": len(failbackBackends(ds, runtime)) > 0,
	}
	s.render(w, r, "node_detail", p)
}

func (s *Server) backendsList(w http.ResponseWriter, r *http.Request) {
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
	p := s.pageBase(r, "Backends", "backends")
	p.Data = map[string]any{
		"Backends": backends,
		"Nodes":    nodes,
		"Catalog":  newCatalog(nodes, backends),
	}
	s.render(w, r, "backends", p)
}

func (s *Server) backendDetail(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	ctx := r.Context()
	id := r.PathValue("id")
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
	p := s.pageBase(r, b.Name, "backends")
	p.Data = map[string]any{
		"Backend": b,
		"Nodes":   nodes,
		"Tunnels": related,
		"Catalog": newCatalog(nodes, []model.Backend{*b}),
	}
	s.render(w, r, "backend_detail", p)
}

func (s *Server) tunnelsList(w http.ResponseWriter, r *http.Request) {
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
	p := s.pageBase(r, "Tunnels", "tunnels")
	p.Data = map[string]any{
		"Rows":     rows,
		"Nodes":    nodes,
		"Backends": backends,
	}
	s.render(w, r, "tunnels", p)
}

func (s *Server) mappingsList(w http.ResponseWriter, r *http.Request) {
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
	p := s.pageBase(r, "Mappings", "mappings")
	p.Data = map[string]any{
		"Mappings": mappings,
		"Nodes":    nodes,
		"Backends": backends,
		"Catalog":  newCatalog(nodes, backends),
	}
	if hx(r) {
		s.render(w, r, "mappings-table", p)
		return
	}
	s.render(w, r, "mappings", p)
}

func (s *Server) sniList(w http.ResponseWriter, r *http.Request) {
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
	p := s.pageBase(r, "SNI routes", "sni")
	p.Data = map[string]any{
		"Rows":     flattenSni(routes, cat),
		"Nodes":    nodes,
		"Backends": backends,
	}
	s.render(w, r, "sni", p)
}

func (s *Server) sniDetail(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	ctx := r.Context()
	route, err := api.GetSniRoute(ctx, r.PathValue("id"))
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
	p := s.pageBase(r, "Edit SNI route", "sni")
	p.Data = map[string]any{
		"Route":          route,
		"Nodes":          nodes,
		"Backends":       backends,
		"DefaultBackend": def,
		"ExtraMatches":   extra,
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
		"Events":   events,
		"Nodes":    nodes,
		"Backends": backends,
		"Filter": map[string]string{
			"Node":    q.Get("node"),
			"Backend": q.Get("backend"),
			"Since":   q.Get("since"),
			"Until":   q.Get("until"),
			"Action":  q.Get("action"),
		},
	}
	s.render(w, r, "events", p)
}

func (s *Server) pageErr(w http.ResponseWriter, r *http.Request, err error) {
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "token") || strings.Contains(strings.ToLower(msg), "bearer") {
		msg = "request failed"
	}
	p := s.pageBase(r, "Error", "")
	p.FlashErr = msg
	s.render(w, r, "error", p)
}
