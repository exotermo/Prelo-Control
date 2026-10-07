package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ApprovalHandler struct {
	decide    *application.DecideApprovalUseCase
	approvals application.ApprovalRepository
	calls     application.ToolCallRepository
	stepUp    stepUpChecker
}

type stepUpChecker interface {
	CheckApprove(ctx context.Context, sessionID uuid.UUID, userID domain.DashboardUserID, approvalID domain.ApprovalRequestID, totpCode string) error
}

// SetStepUp enables G9: HIGH-risk approvals from an app session need a fresh TOTP.
func (h *ApprovalHandler) SetStepUp(stepUp stepUpChecker)                    { h.stepUp = stepUp }
func (h *ApprovalHandler) SetToolCalls(calls application.ToolCallRepository) { h.calls = calls }

func NewApprovalHandler(decide *application.DecideApprovalUseCase, approvals application.ApprovalRepository) *ApprovalHandler {
	return &ApprovalHandler{decide: decide, approvals: approvals}
}

// ListPending is the human-facing queue: every REQUIRE_APPROVAL tool call sits here, with its
// human-readable Scope, until someone approves, denies, or its ExpiresAt passes. Project-scoped
// (Fase W) — projectIdentity(ctx) nil means the "unassigned" bucket.
func (h *ApprovalHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	pending, err := h.approvals.ListPendingByProject(r.Context(), projectIdentity(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]approvalResponse, 0, len(pending))
	for _, approval := range pending {
		response = append(response, approvalResponseFrom(approval))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *ApprovalHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseApprovalID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	approval, err := h.approvals.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approvalResponseFrom(approval))
}

func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decideRequest(w, r, h.decide.Approve, true)
}

func (h *ApprovalHandler) Deny(w http.ResponseWriter, r *http.Request) {
	h.decideRequest(w, r, h.decide.Deny, false)
}

func (h *ApprovalHandler) decideRequest(w http.ResponseWriter, r *http.Request, decide func(ctx context.Context, id domain.ApprovalRequestID, decidedBy string) (domain.ApprovalRequest, error), approving bool) {
	id, err := parseApprovalID(r)
	if err != nil {
		writeError(w, err)
		return
	}

	var req decideApprovalRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, &domain.ValidationError{Message: "invalid request body"})
			return
		}
	}
	decidedBy := req.DecidedBy
	if decidedBy == "" {
		decidedBy = "unknown"
	}
	if identity, ok := FromContext(r.Context()); ok {
		if identity.TokenUse == "dashboard" && identity.Subject != "" {
			decidedBy = identity.Subject
		}
		if approving && h.calls != nil {
			approval, findErr := h.approvals.FindByID(r.Context(), id)
			if findErr == nil && !approval.IsAction() {
				call, callErr := h.calls.FindByID(r.Context(), approval.ToolCallID)
				if callErr == nil {
					if _, isExecutor := application.ExecutorOperation(call.ToolName); isExecutor {
						if identity.TokenUse != "dashboard" || identity.SessionID == "" {
							writeAuthError(w, http.StatusForbidden, "executor approval requires an authenticated dashboard session")
							return
						}
					}
				}
			}
		}
	}
	if identity, ok := FromContext(r.Context()); ok && approving && identity.SessionID != "" {
		if h.stepUp == nil {
			writeError(w, application.ErrStepUpRequired)
			return
		}
		sessionID, _ := uuid.Parse(identity.SessionID)
		userID, _ := uuid.Parse(identity.Subject)
		if err := h.stepUp.CheckApprove(r.Context(), sessionID, domain.DashboardUserID{Value: userID}, id, req.TotpCode); err != nil {
			writeError(w, err)
			return
		}
	}

	approval, err := decide(r.Context(), id, decidedBy)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approvalResponseFrom(approval))
}

// DecideByCode is the owner's WhatsApp answer, relayed by prelo-messaging-bridge (Fase T). Only a
// token with approvals:decide-owner reaches it (the bridge's own), and the bridge only relays
// answers coming from an owner contact. decidedBy records which number answered.
func (h *ApprovalHandler) DecideByCode(w http.ResponseWriter, r *http.Request) {
	var approve bool
	switch r.PathValue("decision") {
	case "approve":
		approve = true
	case "deny":
		approve = false
	default:
		writeError(w, &domain.ValidationError{Message: "decision must be approve or deny"})
		return
	}
	var req decideApprovalRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, &domain.ValidationError{Message: "invalid request body"})
			return
		}
	}
	if req.DecidedBy == "" {
		writeError(w, &domain.ValidationError{Message: "decidedBy is required"})
		return
	}
	approval, err := h.decide.DecideByCode(r.Context(), r.PathValue("code"), approve, req.DecidedBy)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, approvalResponseFrom(approval))
}

func parseApprovalID(r *http.Request) (domain.ApprovalRequestID, error) {
	raw := r.PathValue("approvalId")
	id, err := uuid.Parse(raw)
	if err != nil {
		return domain.ApprovalRequestID{}, &domain.ValidationError{Message: "approvalId must be a valid UUID"}
	}
	return domain.ApprovalRequestID{Value: id}, nil
}
