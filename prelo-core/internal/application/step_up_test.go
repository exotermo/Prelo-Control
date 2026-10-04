package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type stepApprovals struct {
	ApprovalRepository
	approval domain.ApprovalRequest
}

func (f stepApprovals) FindByID(context.Context, domain.ApprovalRequestID) (domain.ApprovalRequest, error) {
	return f.approval, nil
}

type stepCalls struct {
	ToolCallRepository
	call domain.ToolCall
}

func (f stepCalls) FindByID(context.Context, domain.ToolCallID) (domain.ToolCall, error) { return f.call, nil }

type stepSessions struct {
	MobileSessionRepository
	session domain.MobileSession
	touched bool
}

func (f *stepSessions) FindByID(context.Context, uuid.UUID) (domain.MobileSession, error) { return f.session, nil }
func (f *stepSessions) TouchTotp(context.Context, uuid.UUID, time.Time) error {
	f.touched = true
	return nil
}

// G9: approving HIGH risk from the app needs a TOTP from the last 5 minutes; other risks don't.
func TestStepUpPolicy(t *testing.T) {
	ctx := context.Background()
	svc, users, _ := setupDashboardAuth("654321")
	user, _ := domain.NewDashboardUser("owner@example.com", domain.DashboardRoleAdmin)
	user.TOTPEnabled = true
	user.TOTPSecretEncrypted = []byte("secret")
	_ = users.Insert(ctx, user)

	loginAt := time.Now().UTC().Add(-10 * time.Minute)
	session, _ := domain.NewMobileSession(user.ID, uuid.New(), "Moto", "android", loginAt)
	sessions := &stepSessions{session: session}
	svc.SetMobileSessions(sessions)
	call := domain.ToolCall{ID: domain.NewToolCallID(), RiskLevel: domain.RiskHigh}
	policy := NewStepUpPolicy(stepApprovals{approval: domain.ApprovalRequest{ToolCallID: call.ID}}, stepCalls{call: call}, sessions, svc)

	if err := policy.CheckApprove(ctx, session.ID, user.ID, domain.NewApprovalRequestID(), ""); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("HIGH without a recent TOTP must be refused, got %v", err)
	}
	if err := policy.CheckApprove(ctx, session.ID, user.ID, domain.NewApprovalRequestID(), "000000"); !errors.Is(err, ErrStepUpRequired) || sessions.touched {
		t.Fatalf("a wrong code must be refused, got %v", err)
	}
	if err := policy.CheckApprove(ctx, session.ID, user.ID, domain.NewApprovalRequestID(), "654321"); err != nil || !sessions.touched {
		t.Fatalf("a valid code passes and is remembered, got %v", err)
	}
	recent := time.Now().UTC().Add(-time.Minute)
	sessions.session.LastTotpAt = &recent
	if err := policy.CheckApprove(ctx, session.ID, user.ID, domain.NewApprovalRequestID(), ""); err != nil {
		t.Fatalf("a TOTP from a minute ago is enough, got %v", err)
	}
	policy.calls = stepCalls{call: domain.ToolCall{RiskLevel: domain.RiskModerate}}
	sessions.session.LastTotpAt = &loginAt
	if err := policy.CheckApprove(ctx, session.ID, user.ID, domain.NewApprovalRequestID(), ""); err != nil {
		t.Fatalf("MODERATE never needs step-up, got %v", err)
	}
}
