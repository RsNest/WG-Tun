package webui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"transitforge/internal/model"
)

func (f *fakeAPI) ListForeignNodes(context.Context) ([]model.ForeignNode, error) {
	return f.foreign, f.listErr
}
func (f *fakeAPI) GetForeignNode(_ context.Context, id string) (*model.ForeignNode, error) {
	for _, n := range f.foreign {
		if string(n.ID) == id {
			return &n, nil
		}
	}
	return nil, model.NotFound("foreign node", id)
}
func (f *fakeAPI) CreateForeignNode(_ context.Context, input model.ForeignNodeInput) (*model.ForeignNode, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	n := model.ForeignNode{ID: "foreign-1", ForeignNodeInput: input}
	f.foreign = append(f.foreign, n)
	return &n, nil
}
func (f *fakeAPI) PatchForeignNode(ctx context.Context, id string, p model.ForeignNodePatch) (*model.ForeignNode, error) {
	n, err := f.GetForeignNode(ctx, id)
	if err != nil {
		return nil, err
	}
	p.Apply(&n.ForeignNodeInput)
	if err := n.Validate(); err != nil {
		return nil, err
	}
	for i := range f.foreign {
		if f.foreign[i].ID == n.ID {
			f.foreign[i] = *n
		}
	}
	return n, nil
}

func TestForeignNodeFormsAndReadonly(t *testing.T) {
	f := sampleFake()
	s := testUI(t, f)
	op := sessionCookie(t, s, "operator", model.RoleOperator, "op-token")
	form := url.Values{"name": {"cz-01"}, "public_address": {"example.net"}, "country": {"CZ"}, "provider_type": {"SHARX"}, "overlay_addresses": {"10.200.1.2, fd00::2"}}
	w := do(t, s, http.MethodPost, "/foreign-nodes", op, form)
	if w.Code != http.StatusSeeOther || len(f.foreign) != 1 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	f.foreign[0].Labels = map[string]string{"note": "<script>alert(1)</script>"}
	w = do(t, s, http.MethodGet, "/foreign-nodes?id=foreign-1", op, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "cz-01") || strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("detail render or escaping failed")
	}
	form.Set("country", "")
	form.Set("management_address", "")
	w = do(t, s, http.MethodPost, "/foreign-nodes/foreign-1", op, form)
	if w.Code != http.StatusSeeOther || f.foreign[0].Country != "" || f.foreign[0].Labels["note"] == "" {
		t.Fatal("update lost labels or did not clear country")
	}
	form.Set("public_address", "https://invalid.example")
	w = do(t, s, http.MethodPost, "/foreign-nodes/foreign-1", op, form)
	if !strings.Contains(w.Body.String(), "https://invalid.example") || f.foreign[0].PublicAddress != "example.net" {
		t.Fatal("invalid form must preserve submitted fields without mutation")
	}
	ro := sessionCookie(t, s, "reader", model.RoleReadonly, "read-token")
	w = do(t, s, http.MethodGet, "/foreign-nodes?id=foreign-1", ro, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `action="/foreign-nodes/foreign-1"`) {
		t.Fatal("readonly render exposes edit form")
	}
	for _, path := range []string{"/foreign-nodes", "/foreign-nodes/foreign-1"} {
		w = do(t, s, http.MethodPost, path, ro, form)
		if w.Code != http.StatusForbidden {
			t.Fatalf("readonly mutation %s: %d", path, w.Code)
		}
	}
	w = do(t, s, http.MethodGet, "/foreign-nodes?id=missing", op, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown node: %d", w.Code)
	}
}
