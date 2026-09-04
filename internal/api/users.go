package api

import (
	"net/http"

	"transitforge/internal/auth"
	"transitforge/internal/model"
	"transitforge/internal/store"
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p == nil || !auth.CanManageUsers(p.Role) {
		writeErr(w, model.Forbidden("only administrator can list users"))
		return
	}
	out, err := s.accounts.List(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicUsers(out))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p == nil || !auth.CanManageUsers(p.Role) {
		writeErr(w, model.Forbidden("only administrator can create users"))
		return
	}
	var req model.UserCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.accounts.Create(r.Context(), req)
	if err != nil {
		s.audit(r, "create", "user", "", err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "create", "user", string(u.ID), string(u.Role), true)
	writeJSON(w, http.StatusCreated, publicUser(u))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.loadUserForCaller(r, model.ID(r.PathValue("id")))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.loadUserForCaller(r, id); err != nil {
		writeErr(w, err)
		return
	}
	var patch model.UserPatch
	if err := decodeJSON(r, &patch); err != nil {
		writeErr(w, err)
		return
	}
	p := PrincipalFrom(r.Context())
	if p != nil && !auth.CanManageUsers(p.Role) {
		patch.Role = nil
		patch.Disabled = nil
	}
	u, err := s.accounts.Patch(r.Context(), id, patch)
	if err != nil {
		s.audit(r, "update", "user", string(id), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "update", "user", string(id), string(u.Role), true)
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *Server) beginTOTP(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.loadUserForCaller(r, id); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.accounts.BeginTOTP(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "totp-begin", "user", string(id), "", true)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.loadUserForCaller(r, id); err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.accounts.ConfirmTOTP(r.Context(), id, req.Code)
	if err != nil {
		s.audit(r, "totp-confirm", "user", string(id), err.Error(), false)
		writeErr(w, err)
		return
	}
	s.audit(r, "totp-confirm", "user", string(id), "", true)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) disableTOTP(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.loadUserForCaller(r, id); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.accounts.DisableTOTP(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "totp-disable", "user", string(id), "", true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) rotateRecovery(w http.ResponseWriter, r *http.Request) {
	id := model.ID(r.PathValue("id"))
	if _, err := s.loadUserForCaller(r, id); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.accounts.RotateRecoveryCodes(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.audit(r, "recovery-rotate", "user", string(id), "", true)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadUserForCaller(r *http.Request, id model.ID) (*model.User, error) {
	p := PrincipalFrom(r.Context())
	if p == nil {
		return nil, model.Unauthorized("no principal")
	}
	if p.Role == model.RoleAgent {
		return nil, model.Forbidden("agent cannot access users")
	}
	u, err := s.accounts.Get(r.Context(), id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, model.NotFound("user", string(id))
		}
		return nil, err
	}
	if auth.CanManageUsers(p.Role) {
		return u, nil
	}
	if p.UserID != "" && p.UserID == u.ID {
		return u, nil
	}
	return nil, model.Forbidden("cannot access another user")
}

func publicUser(u *model.User) model.User {
	if u == nil {
		return model.User{}
	}
	cp := *u
	cp.PasswordHash = ""
	cp.TOTPSecret = ""
	cp.TOTPPending = ""
	return cp
}

func publicUsers(in []model.User) []model.User {
	out := make([]model.User, 0, len(in))
	for i := range in {
		out = append(out, publicUser(&in[i]))
	}
	return out
}
