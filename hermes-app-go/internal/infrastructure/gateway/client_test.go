package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testConfig(baseURL string) Config {
	return Config{
		BaseURL:        baseURL,
		JWTSecret:      "test-secret-at-least-32-bytes-long!!",
		Issuer:         "hermes-dev",
		Audience:       "llm-gateway",
		RequestTimeout: 500 * time.Millisecond,
		ConnectTimeout: 200 * time.Millisecond,
	}
}

func TestClient_Chat_HappyPath(t *testing.T) {
	var gotPath, gotAuth, gotRequestID string
	var gotBody ChatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotRequestID = r.Header.Get("X-Request-Id")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ChatResponse{
			ID: "resp-1", Provider: "anthropic", Model: "claude-x", Content: "hello",
			Usage: Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}, DurationMs: 42, RequestID: "req-1",
		})
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	req := ChatRequest{
		ModelProfile: "general-chat",
		Messages:     []Message{{Role: "system", Content: "be helpful"}, {Role: "user", Content: "hi"}},
		Parameters:   nil,
		Metadata:     map[string]string{"taskId": "t1", "agentId": "general"},
	}

	resp, err := client.Chat(context.Background(), req, "req-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" || resp.Provider != "anthropic" || resp.Model != "claude-x" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if gotPath != "/api/v1/llm/chat" {
		t.Fatalf("expected path /api/v1/llm/chat, got %s", gotPath)
	}
	if gotRequestID != "req-1" {
		t.Fatalf("expected X-Request-Id req-1, got %s", gotRequestID)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("expected Bearer auth header, got %s", gotAuth)
	}
	if gotBody.ModelProfile != "general-chat" || len(gotBody.Messages) != 2 || gotBody.Metadata["taskId"] != "t1" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}

	tokenStr := strings.TrimPrefix(gotAuth, "Bearer ")
	parsed, err := jwt.Parse(tokenStr, func(*jwt.Token) (interface{}, error) {
		return []byte("test-secret-at-least-32-bytes-long!!"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("expected a valid HS256 JWT, got err=%v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["sub"] != "hermes-app" || claims["iss"] != "hermes-dev" || claims["aud"] != "llm-gateway" || claims["client_id"] != "hermes-app" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestClient_Chat_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_model_profile"}`))
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), ChatRequest{}, "req-1")

	var callErr *CallError
	if err == nil {
		t.Fatal("expected an error")
	}
	if ce, ok := err.(*CallError); ok {
		callErr = ce
	} else {
		t.Fatalf("expected *CallError, got %T: %v", err, err)
	}
	if callErr.Status != http.StatusBadRequest || callErr.Message != "Gateway rejected LLM request" {
		t.Fatalf("unexpected call error: %+v", callErr)
	}
}

func TestClient_Chat_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(testConfig(server.URL))
	_, err := client.Chat(context.Background(), ChatRequest{}, "req-1")

	callErr, ok := err.(*CallError)
	if !ok {
		t.Fatalf("expected *CallError, got %T: %v", err, err)
	}
	if callErr.Status != 504 || callErr.Message != "Gateway did not respond in time" {
		t.Fatalf("unexpected call error: %+v", callErr)
	}
}

func TestClient_Chat_TransportFailure(t *testing.T) {
	// Nothing listening on this port — connection refused, a non-timeout transport error.
	client := NewClient(testConfig("http://127.0.0.1:1"))
	_, err := client.Chat(context.Background(), ChatRequest{}, "req-1")

	callErr, ok := err.(*CallError)
	if !ok {
		t.Fatalf("expected *CallError, got %T: %v", err, err)
	}
	if callErr.Status != 502 || callErr.Message != "Gateway communication failed" {
		t.Fatalf("unexpected call error: %+v", callErr)
	}
}
