package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

// PR-3 (docs/integracoes/action-requests.md).
var (
	ErrActionRequestNotFound = errors.New("action request not found")
	ErrIdempotencyConflict   = errors.New("idempotency key already used with a different payload")
	ErrActionNotApproved     = errors.New("the action request is not approved")
	ErrActionWindowClosed    = errors.New("the approval is too old to start executing")
)

// ActionView is an action request with its approval (status, code, deadline, decision).
type ActionView struct {
	Action   domain.ActionRequest
	Approval domain.ApprovalRequest
}

type ActionRequestRepository interface {
	// InsertWithApproval stores the request and its approval atomically — never one without the other.
	InsertWithApproval(ctx context.Context, action domain.ActionRequest, approval domain.ApprovalRequest) error
	FindByID(ctx context.Context, id uuid.UUID) (ActionView, error)
	FindByIdempotencyKey(ctx context.Context, projectID domain.ProjectID, key string) (ActionView, bool, error)
	ListByProject(ctx context.Context, projectID domain.ProjectID, kind string, limit int) ([]ActionView, error)
	// RecordResult appends a result (idempotent by sequence) and moves the latest only forward.
	RecordResult(ctx context.Context, id uuid.UUID, result domain.ActionResult) error
}
