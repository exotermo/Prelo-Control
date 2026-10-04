package domain

import (
	"time"

	"github.com/google/uuid"
)

type ApprovalRequestID struct{ Value uuid.UUID }

func NewApprovalRequestID() ApprovalRequestID { return ApprovalRequestID{Value: uuid.New()} }

func (id ApprovalRequestID) String() string { return id.Value.String() }

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "PENDING"
	ApprovalApproved ApprovalStatus = "APPROVED"
	ApprovalDenied   ApprovalStatus = "DENIED"
	ApprovalExpired  ApprovalStatus = "EXPIRED"
)

// ApprovalRequest is created exactly once per ToolCall that PermissionPolicy marked
// REQUIRE_APPROVAL (ADR-004: "Aprovações são vinculadas a ação, escopo e expiração"). It is
// never reused: a denied or expired request stays terminal forever, and a fresh tool call —
// even with identical arguments — always creates its own ToolCall and its own
// ApprovalRequest, so there is no way to "cash in" a stale yes for a new action.
type ApprovalRequest struct {
	ID          ApprovalRequestID
	ToolCallID  ToolCallID
	Scope       string // human-readable description of exactly what would run, shown to the approver verbatim rather than a structure they'd need to re-derive.
	Status      ApprovalStatus
	RequestedAt time.Time
	ExpiresAt   time.Time
	DecidedAt   *time.Time
	DecidedBy   *string
	Version     int64
}

func NewApprovalRequest(toolCallID ToolCallID, scope string, ttl time.Duration) ApprovalRequest {
	now := time.Now().UTC()
	return ApprovalRequest{
		ID: NewApprovalRequestID(), ToolCallID: toolCallID, Scope: scope,
		Status: ApprovalPending, RequestedAt: now, ExpiresAt: now.Add(ttl),
	}
}

// EffectiveStatus computes expiry lazily, as a pure function of stored timestamps — a PENDING
// row whose ExpiresAt has passed reads as EXPIRED without needing a background sweeper to have
// touched it yet, the same way ExecutionJob's claimability is a pure function of its own
// timestamps rather than something a sweeper tick must first "apply".
func (a ApprovalRequest) EffectiveStatus(now time.Time) ApprovalStatus {
	if a.Status == ApprovalPending && now.After(a.ExpiresAt) {
		return ApprovalExpired
	}
	return a.Status
}

func (a ApprovalRequest) Approve(decidedBy string) (ApprovalRequest, error) {
	now := time.Now().UTC()
	if current := a.EffectiveStatus(now); current != ApprovalPending {
		return ApprovalRequest{}, &InvalidTransitionError{Entity: "ApprovalRequest", From: string(current), To: string(ApprovalApproved)}
	}
	a.Status = ApprovalApproved
	a.DecidedAt = &now
	a.DecidedBy = &decidedBy
	return a, nil
}

func (a ApprovalRequest) Deny(decidedBy string) (ApprovalRequest, error) {
	now := time.Now().UTC()
	if current := a.EffectiveStatus(now); current != ApprovalPending {
		return ApprovalRequest{}, &InvalidTransitionError{Entity: "ApprovalRequest", From: string(current), To: string(ApprovalDenied)}
	}
	a.Status = ApprovalDenied
	a.DecidedAt = &now
	a.DecidedBy = &decidedBy
	return a, nil
}

func (a ApprovalRequest) WithVersion(v int64) ApprovalRequest { a.Version = v; return a }
