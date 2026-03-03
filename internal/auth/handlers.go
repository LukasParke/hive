package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Service) Router() chi.Router {
	r := chi.NewRouter()
	r.Post("/sign-up/email", s.handleSignUp)
	r.Post("/sign-in/email", s.handleSignIn)
	r.Post("/sign-out", s.handleSignOut)
	r.Get("/get-session", s.handleGetSession)
	return r
}

type signUpRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	User    *User    `json:"user"`
	Session *Session `json:"session"`
}

func (s *Service) handleSignUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeAuthError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if len(req.Password) < 8 {
		writeAuthError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	user, err := s.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			writeAuthError(w, http.StatusConflict, err.Error())
			return
		}
		s.log.Errorf("register: %v", err)
		writeAuthError(w, http.StatusInternalServerError, "registration failed")
		return
	}

	session, _, err := s.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		s.log.Errorf("auto-login after register: %v", err)
		writeAuthJSON(w, http.StatusCreated, user)
		return
	}

	setSessionCookie(w, session.Token, session.ExpiresAt)
	writeAuthJSON(w, http.StatusCreated, sessionResponse{User: user, Session: session})
}

func (s *Service) handleSignIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeAuthError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	session, user, err := s.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeAuthError(w, http.StatusUnauthorized, err.Error())
			return
		}
		s.log.Errorf("login: %v", err)
		writeAuthError(w, http.StatusInternalServerError, "login failed")
		return
	}

	setSessionCookie(w, session.Token, session.ExpiresAt)
	writeAuthJSON(w, http.StatusOK, sessionResponse{User: user, Session: session})
}

func (s *Service) handleSignOut(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		writeAuthJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	_ = s.Logout(r.Context(), cookie.Value)
	clearSessionCookie(w)
	writeAuthJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) handleGetSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		writeAuthJSON(w, http.StatusOK, sessionResponse{})
		return
	}

	session, user, err := s.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		clearSessionCookie(w)
		writeAuthJSON(w, http.StatusOK, sessionResponse{})
		return
	}

	writeAuthJSON(w, http.StatusOK, sessionResponse{User: user, Session: session})
}

func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func writeAuthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	writeAuthJSON(w, status, map[string]map[string]string{
		"error": {"message": msg},
	})
}
