package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chatjpt-org/chatjpt-api/internal/auth"
	"github.com/chatjpt-org/chatjpt-api/internal/store"
)

func TestServerIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(database.Close)

	password := "integration-password"
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	identifier := time.Now().UnixNano()
	firstUsername := fmt.Sprintf("api-first-%d", identifier)
	secondUsername := fmt.Sprintf("api-second-%d", identifier)
	if err := database.CreateUser(ctx, firstUsername, passwordHash); err != nil {
		t.Fatalf("CreateUser(first) error = %v", err)
	}
	if err := database.CreateUser(ctx, secondUsername, passwordHash); err != nil {
		t.Fatalf("CreateUser(second) error = %v", err)
	}

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			if r.Header.Get("CF-Access-Client-Id") != "client-id" || r.Header.Get("CF-Access-Client-Secret") != "client-secret" {
				t.Error("gateway models request is missing Cloudflare Access headers")
			}
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"qwen2.5:1.5b-instruct","object":"model","owned_by":"chatjpt"}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-JChat-User-ID") == "" {
			t.Error("gateway request is missing user ID")
		}
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode gateway request: %v", err)
			return
		}
		if len(request.Messages) == 0 {
			t.Errorf("gateway messages = %#v", request.Messages)
			return
		}
		lastMessage := request.Messages[len(request.Messages)-1]
		if lastMessage.Role != "user" {
			t.Errorf("last gateway message = %#v, want user", lastMessage)
			return
		}
		if lastMessage.Content == "please queue" {
			http.Error(w, "queue full", http.StatusTooManyRequests)
			return
		}
		if len(request.Messages) != 1 || lastMessage.Content != "hello" {
			t.Errorf("gateway messages = %#v", request.Messages)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello back\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(gatewayServer.Close)

	server, err := NewServer(Config{
		Address:         ":0",
		DatabaseURL:     databaseURL,
		CookieSecure:    false,
		SessionDuration: time.Hour,
		GatewayURL:      gatewayServer.URL,
		GatewayAccessID: "client-id",
		GatewaySecret:   "client-secret",
	}, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	apiServer := httptest.NewServer(server.routes())
	t.Cleanup(apiServer.Close)

	loginBody := []byte(fmt.Sprintf(`{"username":%q,"password":%q}`, firstUsername, password))
	loginRequest, err := http.NewRequest(http.MethodPost, apiServer.URL+"/v1/auth/login", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("NewRequest(login) error = %v", err)
	}
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse, err := apiServer.Client().Do(loginRequest)
	if err != nil {
		t.Fatalf("login request error = %v", err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResponse.StatusCode, http.StatusOK)
	}
	loginCookies := loginResponse.Cookies()
	if len(loginCookies) != 1 || loginCookies[0].Name != cookieName {
		t.Fatalf("login cookies = %#v", loginCookies)
	}
	firstCookie := loginCookies[0]

	conversationRequest, err := http.NewRequest(http.MethodPost, apiServer.URL+"/v1/conversations", strings.NewReader(`{"title":"Integration"}`))
	if err != nil {
		t.Fatalf("NewRequest(create conversation) error = %v", err)
	}
	conversationRequest.Header.Set("Content-Type", "application/json")
	conversationRequest.AddCookie(firstCookie)
	createConversationResponse, err := apiServer.Client().Do(conversationRequest)
	if err != nil {
		t.Fatalf("create conversation request error = %v", err)
	}
	defer createConversationResponse.Body.Close()
	if createConversationResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create conversation status = %d, want %d", createConversationResponse.StatusCode, http.StatusCreated)
	}
	var conversation conversationResponse
	if err := json.NewDecoder(createConversationResponse.Body).Decode(&conversation); err != nil {
		t.Fatalf("decode conversation response: %v", err)
	}

	secondUser, _, err := database.FindUserByUsername(ctx, secondUsername)
	if err != nil {
		t.Fatalf("FindUserByUsername(second) error = %v", err)
	}
	secondToken, secondTokenHash, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken() error = %v", err)
	}
	if err := database.CreateSession(ctx, secondUser.ID, secondTokenHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(second) error = %v", err)
	}
	otherUserRequest, err := http.NewRequest(http.MethodGet, apiServer.URL+"/v1/conversations/"+conversation.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest(other user) error = %v", err)
	}
	otherUserRequest.AddCookie(&http.Cookie{Name: cookieName, Value: secondToken})
	otherUserResponse, err := apiServer.Client().Do(otherUserRequest)
	if err != nil {
		t.Fatalf("other user request error = %v", err)
	}
	defer otherUserResponse.Body.Close()
	if otherUserResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("other user status = %d, want %d", otherUserResponse.StatusCode, http.StatusNotFound)
	}
	modelRequest, err := http.NewRequest(http.MethodGet, apiServer.URL+"/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequest(models) error = %v", err)
	}
	modelRequest.AddCookie(firstCookie)
	modelResponse, err := apiServer.Client().Do(modelRequest)
	if err != nil {
		t.Fatalf("models request error = %v", err)
	}
	defer modelResponse.Body.Close()
	if modelResponse.StatusCode != http.StatusOK {
		t.Fatalf("models status = %d, want %d", modelResponse.StatusCode, http.StatusOK)
	}

	messageRequest, err := http.NewRequest(http.MethodPost, apiServer.URL+"/v1/conversations/"+conversation.ID+"/messages", strings.NewReader(`{"content":"hello","max_tokens":64}`))
	if err != nil {
		t.Fatalf("NewRequest(message) error = %v", err)
	}
	messageRequest.Header.Set("Content-Type", "application/json")
	messageRequest.AddCookie(firstCookie)
	streamResponse, err := apiServer.Client().Do(messageRequest)
	if err != nil {
		t.Fatalf("message request error = %v", err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("message status = %d, want %d", streamResponse.StatusCode, http.StatusOK)
	}
	stream, err := io.ReadAll(streamResponse.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(stream), `"delta":"hello back"`) || !strings.Contains(string(stream), "data: [DONE]") {
		t.Fatalf("stream = %q", stream)
	}

	messagesRequest, err := http.NewRequest(http.MethodGet, apiServer.URL+"/v1/conversations/"+conversation.ID+"/messages", nil)
	if err != nil {
		t.Fatalf("NewRequest(messages) error = %v", err)
	}
	messagesRequest.AddCookie(firstCookie)
	messagesResponse, err := apiServer.Client().Do(messagesRequest)
	if err != nil {
		t.Fatalf("messages request error = %v", err)
	}
	defer messagesResponse.Body.Close()
	var listed struct {
		Data []messageResponse `json:"data"`
	}
	if err := json.NewDecoder(messagesResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("decode messages response: %v", err)
	}
	if len(listed.Data) != 2 || listed.Data[0].Content != "hello" || listed.Data[1].Content != "hello back" {
		t.Fatalf("messages response = %#v", listed.Data)
	}

	busyRequest, err := http.NewRequest(http.MethodPost, apiServer.URL+"/v1/conversations/"+conversation.ID+"/messages", strings.NewReader(`{"content":"please queue"}`))
	if err != nil {
		t.Fatalf("NewRequest(busy message) error = %v", err)
	}
	busyRequest.Header.Set("Content-Type", "application/json")
	busyRequest.AddCookie(firstCookie)
	busyResponse, err := apiServer.Client().Do(busyRequest)
	if err != nil {
		t.Fatalf("busy message request error = %v", err)
	}
	defer busyResponse.Body.Close()
	busyStream, err := io.ReadAll(busyResponse.Body)
	if err != nil {
		t.Fatalf("read busy stream: %v", err)
	}
	if busyResponse.StatusCode != http.StatusOK || !strings.Contains(string(busyStream), `"code":"model_busy"`) {
		t.Fatalf("busy response status = %d, stream = %q", busyResponse.StatusCode, busyStream)
	}

	messagesRequest, err = http.NewRequest(http.MethodGet, apiServer.URL+"/v1/conversations/"+conversation.ID+"/messages", nil)
	if err != nil {
		t.Fatalf("NewRequest(messages after busy response) error = %v", err)
	}
	messagesRequest.AddCookie(firstCookie)
	messagesResponse, err = apiServer.Client().Do(messagesRequest)
	if err != nil {
		t.Fatalf("messages after busy response request error = %v", err)
	}
	defer messagesResponse.Body.Close()
	listed = struct {
		Data []messageResponse `json:"data"`
	}{}
	if err := json.NewDecoder(messagesResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("decode messages after busy response: %v", err)
	}
	if len(listed.Data) != 3 || listed.Data[2].Role != "user" || listed.Data[2].Content != "please queue" {
		t.Fatalf("messages after busy response = %#v", listed.Data)
	}
}
