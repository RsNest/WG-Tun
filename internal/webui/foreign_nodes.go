package webui

import (
	"context"
	"net/http"
	"strings"

	"transitforge/internal/model"
	"transitforge/internal/webui/i18n"
)

func (a *liveAPI) ListForeignNodes(ctx context.Context) ([]model.ForeignNode, error) {
	return a.c.ListForeignNodes(ctx)
}
func (a *liveAPI) GetForeignNode(ctx context.Context, id string) (*model.ForeignNode, error) {
	return a.c.GetForeignNode(ctx, id)
}
func (a *liveAPI) CreateForeignNode(ctx context.Context, n model.ForeignNodeInput) (*model.ForeignNode, error) {
	return a.c.CreateForeignNode(ctx, n)
}
func (a *liveAPI) PatchForeignNode(ctx context.Context, id string, p model.ForeignNodePatch) (*model.ForeignNode, error) {
	return a.c.PatchForeignNode(ctx, id, p)
}

func (s *Server) foreignNodes(w http.ResponseWriter, r *http.Request) {
	s.renderForeignNodes(w, r, nil, "")
}

func (s *Server) renderForeignNodes(w http.ResponseWriter, r *http.Request, form map[string]string, formErr string) {
	api := s.api(r)
	nodes, err := api.ListForeignNodes(r.Context())
	if err != nil {
		s.pageErr(w, r, err)
		return
	}
	id := firstNonEmpty(r.PathValue("id"), r.URL.Query().Get("id"))
	var selected *model.ForeignNode
	if id != "" {
		selected, err = api.GetForeignNode(r.Context(), id)
		if err != nil {
			s.pageErr(w, r, err)
			return
		}
	}
	if form == nil {
		form = map[string]string{"provider_type": "UNMANAGED"}
		if selected != nil {
			form = map[string]string{"name": selected.Name, "public_address": selected.PublicAddress, "management_address": selected.ManagementAddress, "country": selected.Country, "provider_type": string(selected.ProviderType), "overlay_addresses": strings.Join(selected.OverlayAddresses, ", ")}
		}
	}
	p := s.pageBase(r, i18n.T(s.locale(r), "foreign.title"), "foreign-nodes")
	p.Data = map[string]any{"ForeignNodes": nodes, "ForeignNode": selected, "SelectedID": id, "ShowCreate": id == "" && (r.URL.Query().Get("new") == "1" || r.Method == http.MethodPost), "Form": form, "FormError": formErr}
	s.render(w, r, "foreign_nodes", p)
}

func (s *Server) saveForeignNode(w http.ResponseWriter, r *http.Request) {
	if !s.requireWrite(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.pageErr(w, r, model.Validation("invalid form"))
		return
	}
	input := model.ForeignNodeInput{Name: r.FormValue("name"), PublicAddress: r.FormValue("public_address"), ManagementAddress: r.FormValue("management_address"), Country: r.FormValue("country"), ProviderType: model.ProviderType(r.FormValue("provider_type")), OverlayAddresses: []string{}}
	if raw := strings.TrimSpace(r.FormValue("overlay_addresses")); raw != "" {
		input.OverlayAddresses = strings.Split(raw, ",")
	}
	id := r.PathValue("id")
	var out *model.ForeignNode
	var err error
	if id == "" {
		out, err = s.api(r).CreateForeignNode(r.Context(), input)
	} else {
		// Labels are not edited by this form and remain untouched.
		patch := model.ForeignNodePatch{Name: &input.Name, PublicAddress: &input.PublicAddress, ManagementAddress: &input.ManagementAddress, Country: &input.Country, ProviderType: &input.ProviderType, OverlayAddresses: &input.OverlayAddresses}
		out, err = s.api(r).PatchForeignNode(r.Context(), id, patch)
	}
	if err != nil {
		s.renderForeignNodes(w, r, formMap(r), s.apiErr(err))
		return
	}
	s.flash(r, "foreign.saved", "")
	s.redirect(w, r, "/foreign-nodes?id="+string(out.ID))
}
