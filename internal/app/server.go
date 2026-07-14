package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chatjpt-org/chatjpt-api/internal/auth"
	"github.com/chatjpt-org/chatjpt-api/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	cookieName      = "chatjpt_session"
	maxRequestBytes = 1 << 20
)

type Server struct {
	config Config
	store  *store.Store
	logger *slog.Logger
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type apiError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func NewServer(config Config, store *store.Store, logger *slog.Logger) *Server {
	return &Server{config: config, store: store, logger: logger}
}

func (s *Server) Serve() error {
	server := &http.Server{
		Addr:              s.config.Address,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.logger.Info("starting ChatJPT API", "address", s.config.Address)
	return server.ListenAndServe()
}

func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", s.health)
	router.Route("/v1/auth", func(router chi.Router) {
		router.Post("/login", s.login)
		router.Post("/logout", s.logout)
		router.Get("/session", s.session)
	})
	return router
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.logger.Warn("health check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "database is unavailable", "service_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if request.Username == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required", "invalid_request")
		return
	}

	user, passwordHash, err := s.store.FindUserByUsername(r.Context(), request.Username)
	if err != nil || !passwordMatches(passwordHash, request.Password) {
		if err != nil && !store.IsNotFound(err) {
			s.logger.Error("find user during login", "error", err)
		}
		writeError(w, http.StatusUnauthorized, "invalid username or password", "invalid_credentials")
		return
	}

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		s.logger.Error("create session token", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create session", "server_error")
		return
	}
	expiresAt := time.Now().Add(s.config.SessionDuration)
	if err := s.store.CreateSession(r.Context(), user.ID, tokenHash, expiresAt); err != nil {
		s.logger.Error("persist session", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create session", "server_error")
		return
	}

	http.SetCookie(w, sessionCookie(token, s.config.CookieSecure, s.config.SessionDuration))
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil && cookie.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashSessionToken(cookie.Value)); err != nil {
			s.logger.Error("delete session", "error", err)
			writeError(w, http.StatusInternalServerError, "could not end session", "server_error")
			return
		}
	}

	http.SetCookie(w, expiredSessionCookie(s.config.CookieSecure))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username})
}

func (s *Server) authenticatedUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "authentication is required", "unauthenticated")
		return store.User{}, false
	}

	user, err := s.store.FindUserBySession(r.Context(), auth.HashSessionToken(cookie.Value))
	if err != nil {
		if !store.IsNotFound(err) {
			s.logger.Error("find session", "error", err)
		}
		writeError(w, http.StatusUnauthorized, "authentication is required", "unauthenticated")
		return store.User{}, false
	}
	return user, true
}

func passwordMatches(encodedHash, password string) bool {
	matches, err := auth.VerifyPassword(encodedHash, password)
	return err == nil && matches
}

func sessionCookie(token string, secure bool, duration time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(duration.Seconds()),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func expiredSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request", "invalid_request")
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object", "invalid_request")
		return errors.New("multiple JSON values")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, apiError{Error: errorBody{Message: message, Code: code}})
}
