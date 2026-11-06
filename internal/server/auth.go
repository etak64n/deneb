package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/etak64n/deneb/internal/app"
	"github.com/etak64n/deneb/internal/store"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// SetupCode is what deneb writes to its log until it has an administrator.
	SetupCode string `json:"setup_code,omitempty"`
}

type userView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// authState tells the dashboard whether to show the setup, the sign-in, or the library.
func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	setup, err := s.app.SetupRequired(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	state := map[string]any{"setup_required": setup, "user": nil, "captions": s.app.Config().Captions.Enabled}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if user, err := s.app.SessionUser(r.Context(), cookie.Value); err == nil {
			state["user"] = userView{ID: user.ID, Email: user.Email}
		}
	}
	writeJSON(w, http.StatusOK, state)
}

// setup creates the first administrator. It works only while deneb has none.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(dashboardHeader) == "" {
		writeError(w, http.StatusForbidden, "requests of the dashboard need the "+dashboardHeader+" header")
		return
	}
	// The limit of sign-in attempts also keeps anyone from guessing the setup code.
	if !s.limiter.allow(limitKey(s.clientIP(r)), time.Now()) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait ten minutes")
		return
	}
	var body credentials
	if !readJSON(w, r, &body) {
		return
	}
	user, token, err := s.app.Setup(r.Context(), body.Email, body.Password, body.SetupCode)
	switch {
	case errors.Is(err, app.ErrSetupDone):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, app.ErrInvalidSetupCode):
		writeError(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("created the administrator", "user", user.ID)
	s.startSession(w, r, token)
	writeJSON(w, http.StatusCreated, map[string]any{"user": userView{ID: user.ID, Email: user.Email}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(dashboardHeader) == "" {
		writeError(w, http.StatusForbidden, "requests of the dashboard need the "+dashboardHeader+" header")
		return
	}
	if !s.limiter.allow(limitKey(s.clientIP(r)), time.Now()) {
		writeError(w, http.StatusTooManyRequests, "too many attempts; wait ten minutes")
		return
	}
	var body credentials
	if !readJSON(w, r, &body) {
		return
	}
	user, token, err := s.app.Login(r.Context(), body.Email, body.Password)
	if errors.Is(err, app.ErrInvalidLogin) {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.startSession(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": userView{ID: user.ID, Email: user.Email}})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err := s.app.Logout(r.Context(), cookie.Value); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Error("end a session", "error", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(app.SessionTTL.Seconds()),
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
}
