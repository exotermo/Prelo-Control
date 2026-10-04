package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
)

type ChatHandler struct {
	service *application.ChatService
}

func NewChatHandler(service *application.ChatService) *ChatHandler {
	return &ChatHandler{service: service}
}

func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid request body"})
		return
	}
	if req.Model == "" {
		writeError(w, &domain.ValidationError{Message: "model is required"})
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, &domain.ValidationError{Message: "messages must not be empty"})
		return
	}

	requestID := r.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = uuid.NewString()
	}

	messages := make([]gateway.Message, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = gateway.Message{Role: m.Role, Content: m.Content}
	}
	var parameters *gateway.Parameters
	if req.Parameters != nil {
		parameters = &gateway.Parameters{Temperature: req.Parameters.Temperature, MaxTokens: req.Parameters.MaxTokens}
	}

	gwReq := gateway.ChatRequest{
		ModelProfile: req.Model,
		Messages:     messages,
		Parameters:   parameters,
		Metadata:     map[string]string{"taskId": req.TaskID, "agentId": req.AgentID},
	}

	resp, err := h.service.Chat(r.Context(), gwReq, requestID, req.TaskID, req.AgentID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
