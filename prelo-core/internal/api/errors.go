package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/bridgeclient"
	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
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
	if errors.Is(err, application.ErrServerNotFound) {
		return http.StatusNotFound, "server_not_found", "server not found"
	}
	if errors.Is(err, application.ErrApiKeyNotFound) {
		return http.StatusNotFound, "api_key_not_found", "api key not found"
	}
	if errors.Is(err, application.ErrWebhookNotFound) {
		return http.StatusNotFound, "webhook_not_found", "webhook not found"
	}
	if errors.Is(err, application.ErrProjectFileNotFound) {
		return http.StatusNotFound, "file_not_found", "file not found"
	}
	// PR-3: external action requests.
	if errors.Is(err, domain.ErrPayloadHashMismatch) {
		return http.StatusUnprocessableEntity, "payload_hash_mismatch", "payloadHash does not match the canonical payload"
	}
	if errors.Is(err, application.ErrIdempotencyConflict) {
		return http.StatusConflict, "idempotency_conflict", "this idempotencyKey was already used with a different payload"
	}
	if errors.Is(err, application.ErrActionNotApproved) {
		return http.StatusConflict, "not_approved", "the action request is not approved — do not execute"
	}
	if errors.Is(err, application.ErrActionWindowClosed) {
		return http.StatusConflict, "approval_window_closed", "the approval is too old to start executing; request again"
	}
	if errors.Is(err, application.ErrActionRequestNotFound) {
		return http.StatusNotFound, "action_request_not_found", "action request not found"
	}
	if errors.Is(err, application.ErrNotProjectMember) {
		return http.StatusForbidden, "forbidden", "not allowed for this project"
	}
	if errors.Is(err, application.ErrStepUpRequired) {
		return http.StatusForbidden, "step_up_required", "approving a high-risk action from the app needs a fresh two-factor code (totpCode)"
	}
	if errors.Is(err, application.ErrMobileSessionNotFound) {
		return http.StatusNotFound, "session_not_found", "session not found"
	}
	if errors.Is(err, application.ErrClientNotFound) {
		return http.StatusNotFound, "client_not_found", "client not found"
	}
	if errors.Is(err, application.ErrContactNotFound) {
		return http.StatusNotFound, "contact_not_found", "contact not found"
	}
	if errors.Is(err, application.ErrContactInUse) {
		return http.StatusConflict, "contact_in_use", "this phone or e-mail already belongs to another client"
	}
	if errors.Is(err, application.ErrProjectNotFound) {
		return http.StatusNotFound, "project_not_found", "project not found"
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
	var bridgeErr *bridgeclient.CallError
	if errors.As(err, &bridgeErr) {
		if bridgeErr.Status == http.StatusUnprocessableEntity {
			return http.StatusUnprocessableEntity, "invalid_phone_number", bridgeErr.Message
		}
		return http.StatusBadGateway, "bridge_failure", bridgeErr.Message
	}
	if status, code, message, ok := mapDashboardAuthError(err); ok {
		return status, code, message
	}
	return http.StatusInternalServerError, "internal_error", "an unexpected error occurred"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
