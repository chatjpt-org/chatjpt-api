package app

import (
	"errors"
	"net/http"
	"testing"

	"github.com/chatjpt-org/chatjpt-api/internal/gateway"
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
