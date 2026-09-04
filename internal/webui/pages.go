package webui

import (
	"net/http"
	"strings"

	"transitforge/internal/model"
	"transitforge/internal/webui/i18n"
)

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if sess := s.sessionFrom(r); sess != nil {
		if sess.MFAPending {
			http.Redirect(w, r, "/login/mfa", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderAuth(w, r, "login", s.loginErr(r.URL.Query().Get("err")))
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?err=invalid_form", http.StatusSeeOther)
		return
	}
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	token := strings.TrimSpace(r.FormValue("token"))
	if token != "" && user == "" {
		s.loginWithToken(w, r, token)
		return
	}
	if s.accounts == nil || user == "" || pass == "" {
		http.Redirect(w, r, "/login?err=invalid_credentials", http.StatusSeeOther)
		return
	}
	u, err := s.accounts.AuthenticatePassword(r.Context(), user, pass)
	if err != nil {
		http.Redirect(w, r, "/login?err="+loginErrCode(err), http.StatusSeeOther)
		return
	}
	loc := firstNonEmpty(u.Locale, s.locale(r))
	sess, err := s.sessions.put(session{
		UserID:      u.ID,
		Name:        u.Username,
		DisplayName: firstNonEmpty(u.DisplayName, u.Username),
		Role:        u.Role,
		Locale:      i18n.Normalize(loc),
		MFAPending:  u.TOTPEnabled,
	})
	if err != nil {
		http.Redirect(w, r, "/login?err=session_failed", http.StatusSeeOther)
		return
	}
	s.setSessionCookie(w, sess)
	s.setLocaleCookie(w, sess.Locale)
	if u.TOTPEnabled {
		http.Redirect(w, r, "/login/mfa", http.StatusSeeOther)
		return
	}
	_ = s.accounts.TouchLogin(r.Context(), u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) loginWithToken(w http.ResponseWriter, r *http.Request, token string) {
	api := s.newClient
	var client API
	if api != nil {
		client = api(token)
	} else {
		client = newLiveAPI(token, &loopback{h: s.apiHandler})
	}
	me, err := client.Whoami(r.Context())
	if err != nil {
		http.Redirect(w, r, "/login?err=invalid_token", http.StatusSeeOther)
		return
	}
	if me.Role != model.RoleOperator && me.Role != model.RoleReadonly && me.Role != model.RoleAdministrator {
		http.Redirect(w, r, "/login?err=agent_token", http.StatusSeeOther)
		return
	}
	sess, err := s.sessions.put(session{Token: token, Name: me.Name, DisplayName: me.Name, Role: me.Role, Locale: s.locale(r)})
	if err != nil {
		http.Redirect(w, r, "/login?err=session_failed", http.StatusSeeOther)
		return
	}
	s.setSessionCookie(w, sess)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) getMFA(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || !sess.MFAPending {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.renderAuth(w, r, "mfa", s.loginErr(r.URL.Query().Get("err")))
}

func (s *Server) postMFA(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionFrom(r)
	if sess == nil || !sess.MFAPending || s.accounts == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	u, err := s.accounts.Get(r.Context(), sess.UserID)
	if err != nil {
		http.Redirect(w, r, "/login?err=invalid_credentials", http.StatusSeeOther)
		return
	}
	if err := s.accounts.ConsumeMFA(r.Context(), u, r.FormValue("code"), r.FormValue("recovery")); err != nil {
		http.Redirect(w, r, "/login/mfa?err=invalid_mfa", http.StatusSeeOther)
		return
	}
	s.sessions.clearMFA(sess.ID)
	_ = s.accounts.TouchLogin(r.Context(), u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if !s.needsSetup() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.renderAuth(w, r, "setup", s.loginErr(r.URL.Query().Get("err")))
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	if !s.needsSetup() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/setup?err=invalid_form", http.StatusSeeOther)
		return
	}
	if r.FormValue("password") != r.FormValue("password_confirm") {
		http.Redirect(w, r, "/setup?err=mismatch", http.StatusSeeOther)
		return
	}
	u, err := s.accounts.SetupFirstAdministrator(r.Context(), r.FormValue("username"), r.FormValue("display_name"), r.FormValue("password"), s.locale(r))
	if err != nil {
		http.Redirect(w, r, "/setup?err=invalid_form", http.StatusSeeOther)
		return
	}
	sess, err := s.sessions.put(session{UserID: u.ID, Name: u.Username, DisplayName: firstNonEmpty(u.DisplayName, u.Username), Role: u.Role, Locale: i18n.Normalize(u.Locale)})
	if err != nil {
		http.Redirect(w, r, "/login?err=session_failed", http.StatusSeeOther)
		return
	}
	s.setSessionCookie(w, sess)
	s.setLocaleCookie(w, sess.Locale)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderAuth(w http.ResponseWriter, r *http.Request, name, errKey string) {
	loc := s.locale(r)
	p := page{Title: i18n.T(loc, name+".title"), Nav: name, Locale: loc, T: i18n.Translator(loc)}
	if errKey != "" {
		p.FlashErr = i18n.T(loc, "login.error."+errKey)
	}
	s.render(w, r, name, p)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionFrom(r); sess != nil {
		s.sessions.delete(sess.ID)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) loginErr(raw string) string {
	switch strings.ReplaceAll(strings.TrimSpace(raw), " ", "_") {
	case "invalid_form", "token_required", "invalid_token", "invalid_credentials", "agent_token", "session_failed", "invalid_mfa", "disabled", "mismatch":
		return strings.ReplaceAll(strings.TrimSpace(raw), " ", "_")
	case "use_an_operator_or_readonly_token":
		return "agent_token"
	default:
		return ""
	}
}

func loginErrCode(err error) string {
	if err == nil {
		return "invalid_credentials"
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "disabled") {
		return "disabled"
	}
	return "invalid_credentials"
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
	loc := s.locale(r)
	cards := make([]nodeCard, 0, len(nodes))
	healthy, degraded, maps := 0, 0, 0
	for _, n := range nodes {
		runtime, _ := api.GetActualState(ctx, string(n.ID))
		card := buildNodeCard(now, n, mappings, runtime, loc)
		cards = append(cards, card)
		maps += card.MappingCount
		switch card.Status {
		case "healthy":
			healthy++
		case "degraded", "warning":
			degraded++
		}
	}
	events, _ := api.ListEvents(ctx, "")
	if len(events) > 8 {
		events = events[:8]
	}
	p := s.pageBase(r, i18n.T(loc, "dash.title"), "dashboard")
	p.Data = map[string]any{
		"Cards":         cards,
		"EntryCount":    len(nodes),
		"HealthyCount":  healthy,
		"DegradedCount": degraded,
		"MappingCount":  maps,
		"Events":        buildEventRows(events),
	}
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
	loc := s.locale(r)
	rows := make([]nodeCard, 0, len(nodes))
	for _, n := range nodes {
		runtime, _ := api.GetActualState(ctx, string(n.ID))
		rows = append(rows, buildNodeCard(now, n, mappings, runtime, loc))
	}
	p := s.pageBase(r, i18n.T(loc, "nodes.title"), "nodes")
	data := map[string]any{
		"Rows":       rows,
		"SelectedID": queryID(r),
	}
	if id := queryID(r); id != "" {
		if ws, err := s.nodeWorkspace(r, api, id); err == nil {
			for k, v := range ws {
				data[k] = v
			}
		}
	}
	p.Data = data
	s.render(w, r, "nodes", p)
}

func (s *Server) nodeDetail(w http.ResponseWriter, r *http.Request) {
	api := s.api(r)
	id := r.PathValue("id")
	ws, err := s.nodeWorkspace(r, api, id)
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	node := ws["Node"].(*model.Node)
	p := s.pageBase(r, node.Name, "nodes")
	p.Data = ws
	s.render(w, r, "node_detail", p)
}

func (s *Server) nodeWorkspace(r *http.Request, api API, id string) (map[string]any, error) {
	ctx := r.Context()
	node, err := api.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}
	ds, err := api.DesiredState(ctx, id)
	if err != nil {
		return nil, err
	}
	runtime, _ := api.GetActualState(ctx, id)
	planText := "NO CHANGES\n"
	if pv, err := api.Plan(ctx, id); err == nil && pv != nil && pv.Plan != "" {
		planText = pv.Plan
	} else if err != nil {
		return nil, err
	}
	allMappings, err := api.ListMappings(ctx)
	if err != nil {
		return nil, err
	}
	var nodeMappings []model.PortMapping
	for _, m := range allMappings {
		if m.NodeID == node.ID {
			nodeMappings = append(nodeMappings, m)
		}
	}
	backends, err := api.ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	cat := newCatalog([]model.Node{*node}, backends)
	tunnels := make([]tunnelRow, 0, len(ds.Tunnels))
	for _, t := range ds.Tunnels {
		tunnels = append(tunnels, buildTunnelRow(t, cat, findTunnelActual(runtime, t)))
	}
	return map[string]any{
		"Node":        node,
		"Card":        buildNodeCard(s.now(), *node, nodeMappings, runtime, s.locale(r)),
		"Tunnels":     tunnels,
		"Mappings":    nodeMappings,
		"Sni":         flattenSni(ds.SniRoutes, cat),
		"Plan":        buildPlanView(planText),
		"Failback":    failbackBackends(ds, runtime),
		"Catalog":     cat,
		"CanFailback": len(failbackBackends(ds, runtime)) > 0,
		"MgmtAddr":    managementAddr(*node),
		"Labels":      formatLabels(node.Labels),
	}, nil
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
	showCreate := queryNew(r) || (formErr != "" && queryID(r) == "")
	selectedID := queryID(r)
	var selected *model.Backend
	var related []model.Tunnel
	if selectedID != "" && !showCreate {
		for i := range backends {
			if string(backends[i].ID) == selectedID {
				selected = &backends[i]
				break
			}
		}
		if selected != nil {
			if tunnels, err := api.ListTunnels(ctx); err == nil {
				for _, t := range tunnels {
					if t.BackendID == selected.ID {
						related = append(related, t)
					}
				}
			}
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "backends.title"), "backends")
	p.Data = map[string]any{
		"Backends":   backends,
		"Nodes":      nodes,
		"Catalog":    newCatalog(nodes, backends),
		"Form":       form,
		"FormError":  formErr,
		"ShowCreate": showCreate,
		"SelectedID": selectedID,
		"Backend":    selected,
		"Tunnels":    related,
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
	showCreate := queryNew(r) || (formErr != "" && queryID(r) == "")
	selectedID := queryID(r)
	var selected *tunnelRow
	if selectedID != "" && !showCreate {
		for i := range rows {
			if rows[i].ID == selectedID {
				selected = &rows[i]
				break
			}
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "tunnels.title"), "tunnels")
	p.Data = map[string]any{
		"Rows":       rows,
		"Nodes":      nodes,
		"Backends":   backends,
		"Form":       form,
		"FormError":  formErr,
		"ShowCreate": showCreate,
		"SelectedID": selectedID,
		"Selected":   selected,
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
	showCreate := queryNew(r) || (formErr != "" && queryID(r) == "")
	selectedID := queryID(r)
	var selected *model.PortMapping
	if selectedID != "" && !showCreate {
		for i := range mappings {
			if string(mappings[i].ID) == selectedID {
				selected = &mappings[i]
				break
			}
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "mappings.title"), "mappings")
	p.Data = map[string]any{
		"Mappings":   mappings,
		"Nodes":      nodes,
		"Backends":   backends,
		"Catalog":    newCatalog(nodes, backends),
		"Form":       form,
		"FormError":  formErr,
		"ShowCreate": showCreate,
		"SelectedID": selectedID,
		"Selected":   selected,
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
	showCreate := queryNew(r) || (formErr != "" && queryID(r) == "")
	selectedID := queryID(r)
	var selected *model.SniRoute
	def, extra := "", ""
	if selectedID != "" && !showCreate {
		for i := range routes {
			if string(routes[i].ID) == selectedID {
				cp := routes[i]
				selected = &cp
				def, extra = sniMatchesText(cp, cat)
				break
			}
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "sni.title"), "sni")
	p.Data = map[string]any{
		"Rows":           flattenSni(routes, cat),
		"Nodes":          nodes,
		"Backends":       backends,
		"Form":           form,
		"FormError":      formErr,
		"ShowCreate":     showCreate,
		"SelectedID":     selectedID,
		"Route":          selected,
		"DefaultBackend": def,
		"ExtraMatches":   extra,
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
	p := s.pageBase(r, i18n.T(s.locale(r), "sni.edit"), "sni")
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
	events, err := api.ListEvents(ctx, eventsAPIQuery(r))
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	nodes, _ := api.ListNodes(ctx)
	backends, _ := api.ListBackends(ctx)
	q := r.URL.Query()
	rows := filterEventRows(buildEventRows(events), q.Get("q"), q.Get("actor"), q.Get("result"))
	selKey := strings.TrimSpace(q.Get("e"))
	var selected *eventRow
	for i := range rows {
		if rows[i].Key == selKey {
			selected = &rows[i]
			break
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "events.title"), "events")
	p.Data = map[string]any{
		"Events":         rows,
		"Nodes":          nodes,
		"Backends":       backends,
		"Selected":       selected,
		"SelectedKey":    selKey,
		"FilterQuery":    retainQuery(r, "e"),
		"FilterQueryNoE": retainQueryAmp(r, "e"),
		"Filter": map[string]string{
			"Node":    q.Get("node"),
			"Backend": q.Get("backend"),
			"Since":   q.Get("since"),
			"Until":   q.Get("until"),
			"Action":  q.Get("action"),
			"Q":       q.Get("q"),
			"Actor":   q.Get("actor"),
			"Result":  q.Get("result"),
		},
		"FilterActive": q.Get("node") != "" || q.Get("backend") != "" || q.Get("since") != "" || q.Get("until") != "" || q.Get("action") != "" || q.Get("q") != "" || q.Get("actor") != "" || q.Get("result") != "",
	}
	s.render(w, r, "events", p)
}

func (s *Server) pageErr(w http.ResponseWriter, r *http.Request, err error) {
	status, a := classifyUIError(err)
	a = s.localizeAlert(r, a)
	if status == http.StatusUnauthorized {
		if hx(r) {
			w.Header().Set("HX-Redirect", "/login")
			http.Error(w, "session expired", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login?err=invalid_token", http.StatusSeeOther)
		return
	}
	if hx(r) {
		s.renderStatus(w, r, status, "alert", s.pageBase(r, "", "").withAlert(a))
		return
	}
	p := s.pageBase(r, a.Title, "")
	p.Data = a
	s.renderStatus(w, r, status, "error", p)
}
