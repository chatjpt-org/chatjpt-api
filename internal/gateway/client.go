package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	baseURL      *url.URL
	accessID     string
	accessSecret string
	httpClient   *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type ChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	UserID    string
}

type Chunk struct {
	Content      string
	FinishReason string
}

type ResponseError struct {
	StatusCode int
	Message    string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("gateway returned HTTP %d: %s", e.StatusCode, e.Message)
}

type StreamError struct {
	Code    string
	Message string
}

func (e *StreamError) Error() string {
	return fmt.Sprintf("gateway stream error %s: %s", e.Code, e.Message)
}

func New(baseURL, accessID, accessSecret string, httpClient *http.Client) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid gateway URL")
	}
	if strings.TrimSpace(accessID) == "" || strings.TrimSpace(accessSecret) == "" {
		return nil, fmt.Errorf("cloudflare Access credentials are required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: parsedURL, accessID: accessID, accessSecret: accessSecret, httpClient: httpClient}, nil
}

func (c *Client) Stream(ctx context.Context, request ChatRequest, handleChunk func(Chunk) error) error {
	payload := struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		Stream    bool      `json:"stream"`
		MaxTokens int       `json:"max_tokens"`
	}{Model: request.Model, Messages: request.Messages, Stream: true, MaxTokens: request.MaxTokens}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode gateway request: %w", err)
	}

	endpoint := c.baseURL.JoinPath("v1", "chat", "completions")
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create gateway request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest.Header.Set("CF-Access-Client-Id", c.accessID)
	httpRequest.Header.Set("CF-Access-Client-Secret", c.accessSecret)
	httpRequest.Header.Set("X-JChat-User-ID", request.UserID)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("send gateway request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return &ResponseError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return nil
		}
		var event struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("decode gateway SSE event: %w", err)
		}
		if event.Error != nil {
			return &StreamError{Code: event.Error.Type, Message: event.Error.Message}
		}
		for _, choice := range event.Choices {
			finishReason := ""
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
			if err := handleChunk(Chunk{Content: choice.Delta.Content, FinishReason: finishReason}); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read gateway SSE stream: %w", err)
	}
	return fmt.Errorf("gateway stream ended without [DONE]")
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	endpoint := c.baseURL.JoinPath("v1", "models")
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create gateway models request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("CF-Access-Client-Id", c.accessID)
	httpRequest.Header.Set("CF-Access-Client-Secret", c.accessSecret)

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send gateway models request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return nil, &ResponseError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}

	var payload struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode gateway models response: %w", err)
	}
	return payload.Data, nil
}
