package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chatjpt-org/chatjpt-api/internal/gateway"
	"github.com/chatjpt-org/chatjpt-api/internal/store"
)

func TestStreamErrorEvent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "model busy",
			err:  &gateway.ResponseError{StatusCode: http.StatusTooManyRequests},
			code: "model_busy",
		},
		{
			name: "gateway unavailable",
			err:  &gateway.ResponseError{StatusCode: http.StatusServiceUnavailable},
			code: "gateway_unavailable",
		},
		{
			name: "unexpected error",
			err:  errors.New("connection closed"),
			code: "gateway_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamErrorEvent(test.err).Error.Code; got != test.code {
				t.Errorf("streamErrorEvent().Error.Code = %q, want %q", got, test.code)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/unknown", nil))

	for name, want := range map[string]string{
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestModelAccessRestrictsAdminModels(t *testing.T) {
	access := newModelAccess(
		[]string{"qwen2.5:1.5b-instruct"},
		[]string{"qwen3:4b-instruct"},
	)
	member := store.User{Role: store.RoleMember}
	admin := store.User{Role: store.RoleAdmin}

	if !access.allows(member, "qwen2.5:1.5b-instruct") {
		t.Error("member should be allowed to use the member model")
	}
	if access.allows(member, "qwen3:4b-instruct") {
		t.Error("member should not be allowed to use the admin model")
	}
	if !access.allows(admin, "qwen3:4b-instruct") {
		t.Error("admin should be allowed to use the admin model")
	}
}
