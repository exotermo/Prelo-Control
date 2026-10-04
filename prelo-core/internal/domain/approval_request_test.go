package domain

import (
	"testing"
	"time"
)

func TestApprovalRequestApprovePending(t *testing.T) {
	req := NewApprovalRequest(NewToolCallID(), "scope", 15*time.Minute)
	approved, err := req.Approve("alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved.Status != ApprovalApproved {
		t.Fatalf("expected APPROVED, got %s", approved.Status)
	}
	if approved.DecidedBy == nil || *approved.DecidedBy != "alice" {
		t.Fatalf("expected decidedBy to be recorded")
	}
}

func TestApprovalRequestDenyPending(t *testing.T) {
	req := NewApprovalRequest(NewToolCallID(), "scope", 15*time.Minute)
	denied, err := req.Deny("alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if denied.Status != ApprovalDenied {
		t.Fatalf("expected DENIED, got %s", denied.Status)
	}
}

func TestApprovalRequestCannotApproveTwice(t *testing.T) {
	req := NewApprovalRequest(NewToolCallID(), "scope", 15*time.Minute)
	approved, _ := req.Approve("alice")
	if _, err := approved.Approve("bob"); err == nil {
		t.Fatal("expected an error approving an already-decided request")
	}
	if _, err := approved.Deny("bob"); err == nil {
		t.Fatal("expected an error denying an already-decided request")
	}
}

func TestApprovalRequestExpiredCannotBeDecided(t *testing.T) {
	req := NewApprovalRequest(NewToolCallID(), "scope", -1*time.Minute)
	if req.EffectiveStatus(time.Now().UTC()) != ApprovalExpired {
		t.Fatalf("expected EffectiveStatus to read EXPIRED for a past ExpiresAt")
	}
	if _, err := req.Approve("alice"); err == nil {
		t.Fatal("expected an error approving an expired request")
	}
	if _, err := req.Deny("alice"); err == nil {
		t.Fatal("expected an error denying an expired request")
	}
}

func TestApprovalRequestStillPendingBeforeExpiry(t *testing.T) {
	req := NewApprovalRequest(NewToolCallID(), "scope", 15*time.Minute)
	if req.EffectiveStatus(time.Now().UTC()) != ApprovalPending {
		t.Fatalf("expected EffectiveStatus to read PENDING before ExpiresAt")
	}
}
