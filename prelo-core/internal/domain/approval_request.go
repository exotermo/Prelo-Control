package domain

import (
	"crypto/rand"
	"math/big"
	"strings"
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
	// ShortCode (Fase T) identifies the request in a WhatsApp answer ("SIM K7Q2").
	ShortCode string
	// ActionRequestID (PR-3): set when the approval is for an external action request instead of
	// an agent's tool call — exactly one of the two subjects (ToolCallID is zero then).
	ActionRequestID *uuid.UUID
}

// NewActionApproval (PR-3) is the approval of an external action request.
func NewActionApproval(actionRequestID uuid.UUID, scope string, ttl time.Duration) ApprovalRequest {
	a := NewApprovalRequest(ToolCallID{}, scope, ttl)
	id := actionRequestID
	a.ActionRequestID = &id
	return a
}

// IsAction: this approval authorizes an external action, not a tool call.
func (a ApprovalRequest) IsAction() bool { return a.ActionRequestID != nil }

// shortCodeAlphabet has no look-alikes (0/O, 1/I/L, 5/S, 8/B, 2/Z) — the owner types it on a phone.
const shortCodeAlphabet = "ACDEFGHJKMNPQRTUVWXY3479"

// ShortCodeLength: 24^4 ≈ 330k codes, unique among the (few) pending requests at a time.
const ShortCodeLength = 4

func NewShortCode() string {
	var b strings.Builder
	for i := 0; i < ShortCodeLength; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(shortCodeAlphabet))))
		if err != nil {
			panic(err)
		}
		b.WriteByte(shortCodeAlphabet[n.Int64()])
	}
	return b.String()
}

// NormalizeShortCode accepts what a person types ("k7q2", " K7Q2 ") and returns the stored form.
func NormalizeShortCode(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }

// Expire closes a PENDING request whose deadline passed, so the waiting execution can move on.
func (a ApprovalRequest) Expire() (ApprovalRequest, error) {
	now := time.Now().UTC()
	if a.Status != ApprovalPending || !now.After(a.ExpiresAt) {
		return ApprovalRequest{}, &InvalidTransitionError{Entity: "ApprovalRequest", From: string(a.Status), To: string(ApprovalExpired)}
	}
	a.Status = ApprovalExpired
	a.DecidedAt = &now
	by := "system:expired"
	a.DecidedBy = &by
	return a, nil
}

func NewApprovalRequest(toolCallID ToolCallID, scope string, ttl time.Duration) ApprovalRequest {
	now := time.Now().UTC()
	return ApprovalRequest{
		ID: NewApprovalRequestID(), ToolCallID: toolCallID, Scope: scope,
		Status: ApprovalPending, RequestedAt: now, ExpiresAt: now.Add(ttl), ShortCode: NewShortCode(),
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
