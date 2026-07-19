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
	"github.com/chatjpt-org/chatjpt-api/internal/gateway"
	"github.com/chatjpt-org/chatjpt-api/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	cookieName           = "chatjpt_session"
	maxRequestBytes      = 1 << 20
	defaultModel         = "qwen2.5:1.5b-instruct"
	defaultChatMaxTokens = 1024
	maxChatTokens        = 1024
	ssePaddingBytes      = 2048
)

type Server struct {
	config       Config
	store        *store.Store
	logger       *slog.Logger
	loginLimiter *loginLimiter
	gateway      *gateway.Client
	modelAccess  modelAccess
}

type modelAccess struct {
	memberModels map[string]struct{}
	adminModels  map[string]struct{}
	defaultModel string
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

type updateModelVisibilityRequest struct {
	IsPublic bool `json:"is_public"`
}

type adminModelResponse struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	OwnedBy  string `json:"owned_by"`
	IsPublic bool   `json:"is_public"`
}
type userResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type conversationRequest struct {
	Title string `json:"title"`
}

type conversationResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type messageResponse struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Model      string    `json:"model,omitempty"`
	Incomplete bool      `json:"incomplete,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type apiError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func NewServer(config Config, store *store.Store, logger *slog.Logger) (*Server, error) {
	if len(config.MemberModels) == 0 {
		config.MemberModels = []string{defaultModel}
	}
	access := newModelAccess(config.MemberModels, config.AdminModels)

	var client *gateway.Client
	if config.GatewayURL != "" && config.GatewayAccessID != "" && config.GatewaySecret != "" {
		var err error
		client, err = gateway.New(config.GatewayURL, config.GatewayAccessID, config.GatewaySecret, nil)
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		config:       config,
		store:        store,
		logger:       logger,
		loginLimiter: newLoginLimiter(),
		gateway:      client,
		modelAccess:  access,
	}, nil
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
	router.Use(securityHeaders)
	router.Get("/healthz", s.health)
	router.Route("/v1/auth", func(router chi.Router) {
		router.Post("/register", s.register)
		router.Post("/login", s.login)
		router.Post("/logout", s.logout)
		router.Get("/session", s.session)
	})
	router.Get("/v1/models", s.listModels)
	router.Route("/v1/admin", func(router chi.Router) {
		router.Get("/models", s.listAdminModels)
		router.Put("/models/{modelID}", s.updateModelVisibility)
		router.Get("/users", s.listUsers)
		router.Post("/users", s.createAdminUser)
		router.Patch("/users/{username}/role", s.updateUserRole)
	})
	router.Route("/v1/conversations", func(router chi.Router) {
		router.Get("/", s.listConversations)
		router.Post("/", s.createConversation)
		router.Get("/{conversationID}", s.getConversation)
		router.Patch("/{conversationID}", s.renameConversation)
		router.Delete("/{conversationID}", s.deleteConversation)
		router.Get("/{conversationID}/messages", s.listMessages)
		router.Post("/{conversationID}/messages", s.createMessage)
	})
	return router
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

type createMessageRequest struct {
	Content   string `json:"content"`
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
}

type streamEvent struct {
	Delta        string `json:"delta,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Incomplete   bool   `json:"incomplete,omitempty"`
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
	loginKey := loginAttemptKey(r.RemoteAddr, request.Username)
	if s.loginLimiter.isBlocked(loginKey) {
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts; try again later", "too_many_login_attempts")
		return
	}

	user, passwordHash, err := s.store.FindUserByUsername(r.Context(), request.Username)
	if err != nil {
		if !store.IsNotFound(err) {
			s.logger.Error("find user during login", "error", err)
			writeError(w, http.StatusInternalServerError, "could not sign in", "server_error")
			return
		}
		s.loginLimiter.recordFailure(loginKey)
		writeError(w, http.StatusUnauthorized, "invalid username or password", "invalid_credentials")
		return
	}
	if !passwordMatches(passwordHash, request.Password) {
		s.loginLimiter.recordFailure(loginKey)
		writeError(w, http.StatusUnauthorized, "invalid username or password", "invalid_credentials")
		return
	}
	s.loginLimiter.reset(loginKey)

	if !s.writeSession(w, r, user) {
		return
	}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if err := auth.ValidateUsername(request.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	if err := s.store.CreateUser(r.Context(), request.Username, passwordHash); err != nil {
		s.logger.Info("public registration rejected", "username", request.Username, "error", err)
		writeError(w, http.StatusConflict, "username is unavailable", "username_unavailable")
		return
	}
	user, _, err := s.store.FindUserByUsername(r.Context(), request.Username)
	if err != nil {
		s.logger.Error("load registered user", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create account", "server_error")
		return
	}
	if !s.writeSession(w, r, user) {
		return
	}
}

func (s *Server) writeSession(w http.ResponseWriter, r *http.Request, user store.User) bool {
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		s.logger.Error("create session token", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create session", "server_error")
		return false
	}
	expiresAt := time.Now().Add(s.config.SessionDuration)
	if err := s.store.CreateSession(r.Context(), user.ID, tokenHash, expiresAt); err != nil {
		s.logger.Error("persist session", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create session", "server_error")
		return false
	}
	http.SetCookie(w, sessionCookie(token, s.config.CookieSecure, s.config.SessionDuration))
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username, Role: string(user.Role)})
	return true
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
	writeJSON(w, http.StatusOK, userResponse{ID: user.ID, Username: user.Username, Role: string(user.Role)})
}

func (s *Server) listAdminModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticatedAdmin(w, r); !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "AI gateway is not configured", "service_unavailable")
		return
	}
	models, err := s.gateway.Models(r.Context())
	if err != nil {
		s.logger.Warn("list admin gateway models", "error", err)
		writeError(w, http.StatusServiceUnavailable, "the AI model catalog is temporarily unavailable", "gateway_unavailable")
		return
	}
	visibility, err := s.store.ListModelVisibility(r.Context())
	if err != nil {
		s.logger.Error("list model visibility", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list model visibility", "server_error")
		return
	}
	response := make([]adminModelResponse, 0, len(models))
	for _, model := range models {
		response = append(response, adminModelResponse{
			ID:       model.ID,
			Object:   model.Object,
			OwnedBy:  model.OwnedBy,
			IsPublic: s.modelAccess.isPublic(model.ID, visibility),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": response})
}

func (s *Server) updateModelVisibility(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticatedAdmin(w, r); !ok {
		return
	}
	modelID := strings.TrimSpace(chi.URLParam(r, "modelID"))
	if modelID == "" {
		writeError(w, http.StatusBadRequest, "model ID is required", "invalid_request")
		return
	}
	var request updateModelVisibilityRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "AI gateway is not configured", "service_unavailable")
		return
	}
	models, err := s.gateway.Models(r.Context())
	if err != nil {
		s.logger.Warn("list gateway models before update", "error", err)
		writeError(w, http.StatusServiceUnavailable, "the AI model catalog is temporarily unavailable", "gateway_unavailable")
		return
	}
	if !containsModel(models, modelID) {
		writeError(w, http.StatusNotFound, "model not found in the gateway catalog", "not_found")
		return
	}
	if err := s.store.SetModelVisibility(r.Context(), modelID, request.IsPublic); err != nil {
		s.logger.Error("set model visibility", "error", err)
		writeError(w, http.StatusInternalServerError, "could not update model visibility", "server_error")
		return
	}
	writeJSON(w, http.StatusOK, adminModelResponse{ID: modelID, IsPublic: request.IsPublic})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticatedAdmin(w, r); !ok {
		return
	}
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("list users", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list users", "server_error")
		return
	}
	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, userResponse{ID: user.ID, Username: user.Username, Role: string(user.Role)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": response})
}

func (s *Server) createAdminUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticatedAdmin(w, r); !ok {
		return
	}
	var request createAdminRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if err := auth.ValidateUsername(request.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	if err := s.store.CreateUserWithRole(r.Context(), request.Username, passwordHash, store.RoleAdmin); err != nil {
		s.logger.Info("admin account creation rejected", "username", request.Username, "error", err)
		writeError(w, http.StatusConflict, "username is unavailable", "username_unavailable")
		return
	}
	user, _, err := s.store.FindUserByUsername(r.Context(), request.Username)
	if err != nil {
		s.logger.Error("load created admin", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create administrator", "server_error")
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: user.ID, Username: user.Username, Role: string(user.Role)})
}

func (s *Server) updateUserRole(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.authenticatedAdmin(w, r)
	if !ok {
		return
	}
	username := strings.TrimSpace(chi.URLParam(r, "username"))
	if err := auth.ValidateUsername(username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	var request updateUserRoleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	role, err := store.ParseUserRole(request.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}
	target, _, err := s.store.FindUserByUsername(r.Context(), username)
	if err != nil {
		if store.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "user not found", "not_found")
			return
		}
		s.logger.Error("find user for role update", "error", err)
		writeError(w, http.StatusInternalServerError, "could not update user role", "server_error")
		return
	}
	if target.ID == admin.ID {
		writeError(w, http.StatusBadRequest, "administrators cannot change their own role", "invalid_request")
		return
	}
	if err := s.store.SetUserRole(r.Context(), username, role); err != nil {
		s.logger.Error("set user role", "error", err)
		writeError(w, http.StatusInternalServerError, "could not update user role", "server_error")
		return
	}
	writeJSON(w, http.StatusOK, userResponse{ID: target.ID, Username: target.Username, Role: string(role)})
}
func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}

	conversations, err := s.store.ListConversations(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list conversations", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list conversations", "server_error")
		return
	}
	responses := make([]conversationResponse, 0, len(conversations))
	for _, conversation := range conversations {
		responses = append(responses, conversationToResponse(conversation))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": responses})
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}

	var request conversationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	title, err := conversationTitle(request.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	conversation, err := s.store.CreateConversation(r.Context(), user.ID, title)
	if err != nil {
		s.logger.Error("create conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create conversation", "server_error")
		return
	}
	writeJSON(w, http.StatusCreated, conversationToResponse(conversation))
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	user, conversationID, ok := s.authenticatedConversation(w, r)
	if !ok {
		return
	}
	conversation, err := s.store.FindConversation(r.Context(), user.ID, conversationID)
	if err != nil {
		s.writeConversationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationToResponse(conversation))
}

func (s *Server) renameConversation(w http.ResponseWriter, r *http.Request) {
	user, conversationID, ok := s.authenticatedConversation(w, r)
	if !ok {
		return
	}
	var request conversationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	title, err := conversationTitle(request.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "invalid_request")
		return
	}

	conversation, err := s.store.RenameConversation(r.Context(), user.ID, conversationID, title)
	if err != nil {
		s.writeConversationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationToResponse(conversation))
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	user, conversationID, ok := s.authenticatedConversation(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteConversation(r.Context(), user.ID, conversationID); err != nil {
		s.writeConversationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	user, conversationID, ok := s.authenticatedConversation(w, r)
	if !ok {
		return
	}
	messages, err := s.store.ListMessages(r.Context(), user.ID, conversationID)
	if err != nil {
		s.logger.Error("list messages", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list messages", "server_error")
		return
	}
	responses := make([]messageResponse, 0, len(messages))
	for _, message := range messages {
		responses = append(responses, messageResponse{
			ID:         message.ID,
			Role:       message.Role,
			Content:    message.Content,
			Model:      message.Model,
			Incomplete: message.Incomplete,
			CreatedAt:  message.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": responses})
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return
	}
	models, err := s.availableModels(r.Context(), user)
	if err != nil {
		s.logger.Warn("list gateway models", "error", err)
		writeError(w, http.StatusServiceUnavailable, "the AI model catalog is temporarily unavailable", "gateway_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	user, conversationID, ok := s.authenticatedConversation(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "AI gateway is not configured", "service_unavailable")
		return
	}

	var request createMessageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	if request.Content == "" {
		writeError(w, http.StatusBadRequest, "message content is required", "invalid_request")
		return
	}
	if request.MaxTokens == 0 {
		request.MaxTokens = defaultChatMaxTokens
	}
	if request.MaxTokens < 1 || request.MaxTokens > maxChatTokens {
		writeError(w, http.StatusBadRequest, "max_tokens must be between 1 and 1024", "invalid_request")
		return
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		request.Model = s.modelAccess.defaultModel
	}
	allowed, err := s.allowsModel(r.Context(), user, request.Model)
	if err != nil {
		s.logger.Error("load model visibility", "error", err)
		writeError(w, http.StatusInternalServerError, "could not check model access", "server_error")
		return
	}
	if !allowed {
		writeError(w, http.StatusBadRequest, "requested model is not allowed", "model_not_allowed")
		return
	}
	models, err := s.availableModels(r.Context(), user)
	if err != nil {
		s.logger.Warn("list gateway models before generation", "error", err)
		writeError(w, http.StatusServiceUnavailable, "the AI model catalog is temporarily unavailable", "gateway_unavailable")
		return
	}
	if !containsModel(models, request.Model) {
		writeError(w, http.StatusServiceUnavailable, "requested model is not currently available", "model_unavailable")
		return
	}
	if _, err := s.store.CreateMessage(r.Context(), user.ID, conversationID, "user", request.Content, ""); err != nil {
		s.writeConversationError(w, err)
		return
	}
	messages, err := s.store.ListMessages(r.Context(), user.ID, conversationID)
	if err != nil {
		s.logger.Error("list messages for generation", "error", err)
		writeError(w, http.StatusInternalServerError, "could not start generation", "server_error")
		return
	}
	gatewayMessages := make([]gateway.Message, 0, len(messages))
	for _, message := range messages {
		gatewayMessages = append(gatewayMessages, gateway.Message{Role: message.Role, Content: message.Content})
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported", "server_error")
		return
	}
	// The initial SSE comment makes intermediaries start forwarding the stream now,
	// rather than buffering the first small model chunks until the response ends.
	if err := writeSSEPadding(w); err != nil {
		s.logger.Warn("write SSE padding", "error", err)
		return
	}
	flusher.Flush()
	var answer strings.Builder
	finishReason := ""
	err = s.gateway.Stream(r.Context(), gateway.ChatRequest{
		Model:     request.Model,
		Messages:  gatewayMessages,
		MaxTokens: request.MaxTokens,
		UserID:    user.ID,
	}, func(chunk gateway.Chunk) error {
		answer.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
		if err := writeSSE(w, streamEvent{Delta: chunk.Content, FinishReason: chunk.FinishReason}); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	incomplete := finishReason == "length"
	if err != nil {
		if answer.Len() == 0 {
			s.logger.Warn("gateway generation failed", "error", err)
			_ = writeSSE(w, streamErrorEvent(err))
			flusher.Flush()
			return
		}
		incomplete = true
		if finishReason == "" {
			finishReason = "interrupted"
		}
		s.logger.Warn("gateway generation interrupted after partial response", "error", err)
	}

	persistContext, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if _, err := s.store.CreateAssistantMessage(persistContext, user.ID, conversationID, answer.String(), request.Model, incomplete); err != nil {
		s.logger.Error("persist assistant message", "error", err)
		_ = writeSSE(w, apiError{Error: errorBody{Message: "could not save assistant response", Code: "server_error"}})
		flusher.Flush()
		return
	}
	if incomplete {
		_ = writeSSE(w, streamEvent{FinishReason: finishReason, Incomplete: true})
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func (s *Server) availableModels(ctx context.Context, user store.User) ([]gateway.Model, error) {
	if s.gateway == nil {
		return nil, errors.New("AI gateway is not configured")
	}
	visibility, err := s.store.ListModelVisibility(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.gateway.Models(ctx)
	if err != nil {
		return nil, err
	}
	available := make([]gateway.Model, 0, len(models))
	for _, model := range models {
		if s.modelAccess.allowsWithVisibility(user, model.ID, visibility) {
			available = append(available, model)
		}
	}
	return available, nil
}

func (s *Server) allowsModel(ctx context.Context, user store.User, modelID string) (bool, error) {
	visibility, err := s.store.ListModelVisibility(ctx)
	if err != nil {
		return false, err
	}
	return s.modelAccess.allowsWithVisibility(user, modelID, visibility), nil
}

func newModelAccess(memberModels, adminModels []string) modelAccess {
	access := modelAccess{
		memberModels: make(map[string]struct{}, len(memberModels)),
		adminModels:  make(map[string]struct{}, len(adminModels)),
		defaultModel: memberModels[0],
	}
	for _, model := range memberModels {
		access.memberModels[model] = struct{}{}
	}
	for _, model := range adminModels {
		access.adminModels[model] = struct{}{}
	}
	return access
}

func (a modelAccess) isPublic(model string, visibility map[string]bool) bool {
	if isPublic, configured := visibility[model]; configured {
		return isPublic
	}
	_, allowed := a.memberModels[model]
	return allowed
}
func (a modelAccess) allowsWithVisibility(user store.User, model string, visibility map[string]bool) bool {
	if user.Role == store.RoleAdmin {
		return true
	}
	return a.isPublic(model, visibility)
}
func (a modelAccess) allows(user store.User, model string) bool {
	if _, allowed := a.memberModels[model]; allowed {
		return true
	}
	if user.Role != store.RoleAdmin {
		return false
	}
	_, allowed := a.adminModels[model]
	return allowed
}

func containsModel(models []gateway.Model, modelID string) bool {
	for _, model := range models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}

func streamErrorEvent(err error) apiError {
	var streamError *gateway.StreamError
	if errors.As(err, &streamError) {
		switch streamError.Code {
		case "timeout_error":
			return apiError{Error: errorBody{Message: "the AI model reached its generation time limit", Code: "generation_timeout"}}
		case "request_cancelled":
			return apiError{Error: errorBody{Message: "the AI generation was interrupted", Code: "generation_interrupted"}}
		}
	}

	var responseError *gateway.ResponseError
	if errors.As(err, &responseError) {
		switch responseError.StatusCode {
		case http.StatusTooManyRequests, http.StatusConflict:
			return apiError{Error: errorBody{Message: "the AI model is busy; try again shortly", Code: "model_busy"}}
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return apiError{Error: errorBody{Message: "the AI gateway is temporarily unavailable", Code: "gateway_unavailable"}}
		}
	}
	return apiError{Error: errorBody{Message: "generation failed", Code: "gateway_error"}}
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

func (s *Server) authenticatedAdmin(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return store.User{}, false
	}
	if user.Role != store.RoleAdmin {
		writeError(w, http.StatusForbidden, "administrator access is required", "forbidden")
		return store.User{}, false
	}
	return user, true
}
func (s *Server) authenticatedConversation(w http.ResponseWriter, r *http.Request) (store.User, string, bool) {
	user, ok := s.authenticatedUser(w, r)
	if !ok {
		return store.User{}, "", false
	}
	conversationID := chi.URLParam(r, "conversationID")
	if !isUUID(conversationID) {
		writeError(w, http.StatusBadRequest, "conversation ID must be a UUID", "invalid_request")
		return store.User{}, "", false
	}
	return user, conversationID, true
}

func (s *Server) writeConversationError(w http.ResponseWriter, err error) {
	if store.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "conversation not found", "not_found")
		return
	}
	s.logger.Error("conversation operation", "error", err)
	writeError(w, http.StatusInternalServerError, "could not complete conversation operation", "server_error")
}

func conversationTitle(value string) (string, error) {
	title := strings.TrimSpace(value)
	if title == "" {
		return "Nova conversa", nil
	}
	if len(title) > 200 {
		return "", errors.New("title must contain at most 200 characters")
	}
	return title, nil
}

func conversationToResponse(conversation store.Conversation) conversationResponse {
	return conversationResponse{
		ID:        conversation.ID,
		Title:     conversation.Title,
		CreatedAt: conversation.CreatedAt,
		UpdatedAt: conversation.UpdatedAt,
	}
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
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

func writeSSE(w http.ResponseWriter, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte("data: " + string(payload) + "\n\n"))
	return err
}

func writeSSEPadding(w io.Writer) error {
	_, err := io.WriteString(w, ":"+strings.Repeat(" ", ssePaddingBytes)+"\n\n")
	return err
}
