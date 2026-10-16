package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
)

func (s *Server) getSetup(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	needed, err := s.Auth.NeedsSetup()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needed": needed})
	return nil
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request, _ *auth.User) error {
	var req struct {
		Code        string `json:"code"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	u, err := s.Auth.CompleteSetup(req.Code, req.Username, req.DisplayName, req.Password, clientIP(r))
	s.record(r, &u, "setup.complete", req.Username, nil, err)
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		return errorf(http.StatusConflict, "%v", err)
	case errors.Is(err, auth.ErrLockedOut):
		return errorf(http.StatusTooManyRequests, "%v", err)
	case errors.Is(err, auth.ErrBadSetupCode), errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrWeakPassword):
		return errorf(http.StatusBadRequest, "%v", err)
	case err != nil:
		return err
	}
	_, token, err := s.Auth.Login(req.Username, req.Password, clientIP(r))
	if err != nil {
		return err
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, u.Public())
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, _ *auth.User) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	u, token, err := s.Auth.Login(req.Username, req.Password, clientIP(r))
	if err != nil {
		s.record(r, nil, "auth.login", req.Username, nil, err)
		if errors.Is(err, auth.ErrLockedOut) {
			return errorf(http.StatusTooManyRequests, "%v", err)
		}
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return errorf(http.StatusUnauthorized, "%v", err)
		}
		return err
	}
	s.record(r, &u, "auth.login", u.Username, nil, nil)
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, u.Public())
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Auth.Logout(c.Value); err != nil {
			return err
		}
	}
	clearSessionCookie(w, r)
	s.record(r, u, "auth.logout", u.Username, nil, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, u *auth.User) error {
	writeJSON(w, http.StatusOK, u.Public())
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	cookie, _ := r.Cookie(sessionCookie)
	err := s.Auth.ChangePassword(u.ID, req.Current, req.New, cookie.Value)
	s.record(r, u, "auth.password_change", u.Username, nil, err)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return errorf(http.StatusBadRequest, "current password is wrong")
	case errors.Is(err, auth.ErrWeakPassword):
		return errorf(http.StatusBadRequest, "%v", err)
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request, _ *auth.User) error {
	users, err := s.Auth.ListUsers()
	if err != nil {
		return err
	}
	out := make([]auth.Public, 0, len(users))
	for _, u := range users {
		out = append(out, u.Public())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Username    string    `json:"username"`
		DisplayName string    `json:"display_name"`
		Role        auth.Role `json:"role"`
		Password    string    `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	created, err := s.Auth.CreateUser(req.Username, req.DisplayName, req.Role, req.Password)
	s.record(r, u, "user.create", req.Username, map[string]any{"role": req.Role}, err)
	if err != nil {
		return userError(err)
	}
	writeJSON(w, http.StatusCreated, created.Public())
	return nil
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var changes auth.UserChanges
	if err := decode(w, r, &changes); err != nil {
		return err
	}
	id := r.PathValue("id")
	updated, err := s.Auth.UpdateUser(id, changes)
	details := map[string]any{}
	if changes.Role != nil {
		details["role"] = *changes.Role
	}
	if changes.Disabled != nil {
		details["disabled"] = *changes.Disabled
	}
	if changes.DisplayName != nil {
		details["display_name"] = *changes.DisplayName
	}
	s.record(r, u, "user.update", updated.Username+"#"+id, details, err)
	if err != nil {
		return userError(err)
	}
	writeJSON(w, http.StatusOK, updated.Public())
	return nil
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	id := r.PathValue("id")
	err := s.Auth.DeleteUser(id)
	s.record(r, u, "user.delete", id, nil, err)
	if err != nil {
		return userError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request, u *auth.User) error {
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &req); err != nil {
		return err
	}
	id := r.PathValue("id")
	err := s.Auth.ResetPassword(id, req.Password)
	s.record(r, u, "user.password_reset", id, nil, err)
	if err != nil {
		return userError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func userError(err error) error {
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return errorf(http.StatusNotFound, "%v", err)
	case errors.Is(err, auth.ErrUsernameTaken), errors.Is(err, auth.ErrLastOwner):
		return errorf(http.StatusConflict, "%v", err)
	case errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrWeakPassword):
		return errorf(http.StatusBadRequest, "%v", err)
	default:
		var he *httpError
		if errors.As(err, &he) {
			return err
		}
		// Validation messages from the service are safe to show.
		return errorf(http.StatusBadRequest, "%v", err)
	}
}
