package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamSendsAccessHeadersAndParsesChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("CF-Access-Client-Id") != "client-id" || r.Header.Get("CF-Access-Client-Secret") != "client-secret" {
			t.Fatal("missing Cloudflare Access headers")
		}
		if r.Header.Get("X-JChat-User-ID") != "user-1" {
			t.Fatal("missing JChat user header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Ola\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := New(server.URL, "client-id", "client-secret", server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var chunks []Chunk
	err = client.Stream(context.Background(), ChatRequest{Model: "qwen2.5:1.5b-instruct", UserID: "user-1"}, func(chunk Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if len(chunks) != 2 || chunks[0].Content != "Ola" || chunks[1].FinishReason != "stop" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestStreamReturnsResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "queue full", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := New(server.URL, "client-id", "client-secret", server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = client.Stream(context.Background(), ChatRequest{}, func(Chunk) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("Stream() error = %v, want HTTP 429", err)
	}
}
