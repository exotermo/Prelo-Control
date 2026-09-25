package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
)

// writeError maps a domain/application/gateway error to the stable, non-leaking {code,message}
// shape used across the API, mirroring ApiExceptionHandler.java's four handled cases plus the
// Gateway passthrough's gateway_failure case. Anything else falls back to a generic 500.
func writeError(w http.ResponseWriter, err error) {
	status, code, message := mapError(err)
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}

func mapError(err error) (int, string, string) {
	var invalidTransition *domain.InvalidTransitionError
	if errors.As(err, &invalidTransition) {
		if invalidTransition.Entity == "ApprovalRequest" {
			return http.StatusConflict, "invalid_approval_transition", "this approval request was already decided or has expired"
		}
		return http.StatusConflict, "invalid_task_transition", "the task is not in a state that allows this operation"
	}
	if errors.Is(err, application.ErrOptimisticLock) {
		return http.StatusConflict, "concurrent_modification", "the resource was modified concurrently; retry with fresh data"
	}
	var unknownAgent *domain.ErrUnknownAgent
	if errors.As(err, &unknownAgent) {
		return http.StatusBadRequest, "unknown_agent", "agentId does not match any agent in the catalog"
	}
	if errors.Is(err, application.ErrTaskNotFound) {
		return http.StatusNotFound, "task_not_found", "task not found"
	}
	if errors.Is(err, application.ErrExecutionNotFound) {
		return http.StatusNotFound, "execution_not_found", "execution not found"
	}
	if errors.Is(err, application.ErrToolNotFound) {
		return http.StatusNotFound, "tool_not_found", "tool not found, or not registered"
	}
	if errors.Is(err, application.ErrToolCallNotFound) {
		return http.StatusNotFound, "tool_call_not_found", "tool call not found"
	}
	if errors.Is(err, application.ErrApprovalNotFound) {
		return http.StatusNotFound, "approval_not_found", "approval request not found"
	}
	var toolLimit *application.ToolLimitError
	if errors.As(err, &toolLimit) {
		return http.StatusRequestEntityTooLarge, "tool_limit_exceeded", "tool input or output exceeded the configured safety limit"
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		return http.StatusBadRequest, "validation_error", validation.Message
	}
	var callErr *gateway.CallError
	if errors.As(err, &callErr) {
		return http.StatusBadGateway, "gateway_failure", callErr.Message
	}
	return http.StatusInternalServerError, "internal_error", "an unexpected error occurred"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
