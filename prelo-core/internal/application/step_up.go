package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

// StepUpPolicy (PR-2, contratos G9): approving a HIGH-risk action from the app needs a TOTP checked
// within the last few minutes — a stolen, unlocked phone is not enough to authorize a deploy or a
// message to a client. Web sessions and denials are not affected.
type StepUpPolicy struct {
	approvals ApprovalRepository
	calls     ToolCallRepository
	sessions  MobileSessionRepository
	auth      *DashboardAuthService
	now       func() time.Time
}

func NewStepUpPolicy(approvals ApprovalRepository, calls ToolCallRepository, sessions MobileSessionRepository, auth *DashboardAuthService) *StepUpPolicy {
	return &StepUpPolicy{approvals: approvals, calls: calls, sessions: sessions, auth: auth, now: time.Now}
}

func (p *StepUpPolicy) CheckApprove(ctx context.Context, sessionID uuid.UUID, userID domain.DashboardUserID, approvalID domain.ApprovalRequestID, totpCode string) error {
	approval, err := p.approvals.FindByID(ctx, approvalID)
	if err != nil {
		return err
	}
	if !approval.IsAction() { // external action requests (PR-3) are always HIGH
		call, err := p.calls.FindByID(ctx, approval.ToolCallID)
		if err != nil {
			return err
		}
		if call.RiskLevel != domain.RiskHigh {
			return nil
		}
	}
	session, err := p.sessions.FindByID(ctx, sessionID)
	if err != nil {
		return ErrStepUpRequired
	}
	if session.RecentTotp(p.now().UTC()) {
		return nil
	}
	if totpCode == "" {
		return ErrStepUpRequired
	}
	return p.auth.ConfirmStepUp(ctx, sessionID, userID, totpCode)
}
