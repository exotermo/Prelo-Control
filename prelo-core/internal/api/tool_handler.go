package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ToolHandler struct {
	invoke *application.InvokeToolUseCase
	tools  application.ToolRegistry
}

func NewToolHandler(invoke *application.InvokeToolUseCase, tools application.ToolRegistry) *ToolHandler {
	return &ToolHandler{invoke: invoke, tools: tools}
}

// List exposes the curated tool catalog (name, description, risk) — an operator or a future
// agent-side caller needs this to know what exists before ever invoking anything.
func (h *ToolHandler) List(w http.ResponseWriter, r *http.Request) {
	defs := h.tools.List()
	response := make([]toolResponse, 0, len(defs))
	for _, def := range defs {
		response = append(response, toolResponseFrom(def))
	}
	writeJSON(w, http.StatusOK, response)
}

// Invoke is the only HTTP path that can run a tool. It always returns 200 with the resulting
// ToolCall — including when the call is DENIED or left PENDING_APPROVAL, since those are
// successful evaluations, not request errors; the caller reads `decision`/`outcome` to see
// what actually happened.
func (h *ToolHandler) Invoke(w http.ResponseWriter, r *http.Request) {
	rawExecutionID := r.PathValue("executionId")
	executionID, err := uuid.Parse(rawExecutionID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "executionId must be a valid UUID"})
		return
	}
	toolName := r.PathValue("toolName")
	if toolName == "" {
		writeError(w, &domain.ValidationError{Message: "toolName is required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	var req invokeToolRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, &domain.ValidationError{Message: "invalid request body"})
			return
		}
	}
	argsJSON := req.Args
	if argsJSON == "" {
		argsJSON = "{}"
	}

	call, _, err := h.invoke.Invoke(r.Context(), domain.ExecutionID{Value: executionID}, toolName, argsJSON)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toolCallResponseFrom(call))
}
