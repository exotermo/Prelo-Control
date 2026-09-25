package domain

import "testing"

func TestExecutionSuspensionResolved(t *testing.T) {
	suspension := NewExecutionSuspension(NewExecutionID(), SuspensionApproval, "approval-1")
	if suspension.ResolvedAt != nil {
		t.Fatal("expected a fresh suspension to be unresolved")
	}
	resolved := suspension.Resolved()
	if resolved.ResolvedAt == nil {
		t.Fatal("expected Resolved() to set ResolvedAt")
	}
	if resolved.Reason != SuspensionApproval || resolved.ResumeKey != "approval-1" {
		t.Fatalf("expected Reason/ResumeKey to be preserved, got %+v", resolved)
	}
}
