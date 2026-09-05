package api

import (
	"net/http"
	"time"

	"transitforge/internal/ident"
	"transitforge/internal/model"
	"transitforge/internal/store"
)

// Foreign inventory is an operator surface; machine agents have no access.
func (s *Server) foreignAccess(next http.HandlerFunc, mutate bool) http.HandlerFunc {
	return s.authn(func(w http.ResponseWriter, r *http.Request) {
		role := PrincipalFrom(r.Context()).Role
		allowed := role == model.RoleAdministrator || role == model.RoleOperator || (!mutate && role == model.RoleReadonly)
		if !allowed {
			writeErr(w, model.Forbidden("role cannot access foreign node inventory"))
			return
		}
		next(w, r)
	}, mutate)
}

func (s *Server) listForeignNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListForeignNodes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) getForeignNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.GetForeignNode(r.Context(), model.ID(r.PathValue("id")))
	if err != nil {
		foreignNodeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) createForeignNode(w http.ResponseWriter, r *http.Request) {
	var input model.ForeignNodeInput
	if err := decodeJSON(r, &input); err != nil {
		writeErr(w, err)
		return
	}
	if err := input.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	n := model.ForeignNode{ID: ident.New(), ForeignNodeInput: input, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.store.CreateForeignNode(r.Context(), &n); err != nil {
		s.audit(r, "create", "foreign-node", "", err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "foreign-node", string(n.ID), "", true)
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) patchForeignNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.GetForeignNode(r.Context(), model.ID(r.PathValue("id")))
	if err != nil {
		foreignNodeError(w, r, err)
		return
	}
	var patch model.ForeignNodePatch
	if err := decodeJSON(r, &patch); err != nil {
		writeErr(w, err)
		return
	}
	patch.Apply(&n.ForeignNodeInput)
	if err := n.Validate(); err != nil {
		writeErr(w, err)
		return
	}
	n.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateForeignNode(r.Context(), n); err != nil {
		s.audit(r, "update", "foreign-node", string(n.ID), err.Error(), false)
		foreignNodeError(w, r, err)
		return
	}
	s.audit(r, "update", "foreign-node", string(n.ID), "", true)
	writeJSON(w, http.StatusOK, n)
}

func foreignNodeError(w http.ResponseWriter, r *http.Request, err error) {
	if store.IsNotFound(err) {
		err = model.NotFound("foreign node", r.PathValue("id"))
	}
	writeErr(w, err)
}
