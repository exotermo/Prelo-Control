package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
)

type fakeGateway struct {
	resp gateway.ChatResponse
	err  error
}

func (f fakeGateway) Chat(context.Context, gateway.ChatRequest, string) (gateway.ChatResponse, error) {
	return f.resp, f.err
}

type fakeLlmExecutionRepo struct {
	saved []application.LlmExecutionRecord
}

func (f *fakeLlmExecutionRepo) Save(_ context.Context, record application.LlmExecutionRecord) error {
	f.saved = append(f.saved, record)
	return nil
}

func TestChatHandler_Success(t *testing.T) {
	gw := fakeGateway{resp: gateway.ChatResponse{Content: "hi there", Provider: "anthropic", Model: "claude-x"}}
	repo := &fakeLlmExecutionRepo{}
	service := application.NewChatService(gw, repo)
	handler := NewChatHandler(service)

	mux := http.NewServeMux()
	RegisterRoutes(mux, &TaskHandler{}, handler, &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"model":"general-chat","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp gateway.ChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Content != "hi there" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(repo.saved) != 1 || repo.saved[0].Status != "COMPLETED" {
		t.Fatalf("expected one COMPLETED audit record, got %+v", repo.saved)
	}
}

func TestChatHandler_GatewayFailure(t *testing.T) {
	gw := fakeGateway{err: &gateway.CallError{Status: 502, Message: "Gateway communication failed"}}
	repo := &fakeLlmExecutionRepo{}
	service := application.NewChatService(gw, repo)
	handler := NewChatHandler(service)

	mux := http.NewServeMux()
	RegisterRoutes(mux, &TaskHandler{}, handler, &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"model":"general-chat","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Code != "gateway_failure" {
		t.Fatalf("expected code gateway_failure, got %s", errResp.Code)
	}
	if len(repo.saved) != 1 || repo.saved[0].Status != "FAILED" {
		t.Fatalf("expected one FAILED audit record, got %+v", repo.saved)
	}
}

func TestChatHandler_ValidatesEmptyMessages(t *testing.T) {
	service := application.NewChatService(fakeGateway{}, &fakeLlmExecutionRepo{})
	handler := NewChatHandler(service)

	mux := http.NewServeMux()
	RegisterRoutes(mux, &TaskHandler{}, handler, &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"model":"general-chat","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
