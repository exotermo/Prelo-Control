package domain

import "testing"

func TestTurnRequestIDIsDeterministic(t *testing.T) {
	execID := NewExecutionID()
	first := TurnRequestID(execID, 2)
	second := TurnRequestID(execID, 2)
	if first != second {
		t.Fatalf("expected the same (executionID, turnNumber) to always derive the same RequestID, got %q vs %q", first, second)
	}
	different := TurnRequestID(execID, 3)
	if different == first {
		t.Fatal("expected a different turn number to derive a different RequestID")
	}
}

func TestExecutionTurnCompletedSetsOutputAndTimestamp(t *testing.T) {
	turn := NewExecutionTurn(NewExecutionID(), 0, TurnLLMCall, "prompt")
	completed := turn.Completed("answer")
	if completed.Output == nil || *completed.Output != "answer" {
		t.Fatalf("expected output to be set, got %+v", completed.Output)
	}
	if completed.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
	if completed.Error != nil {
		t.Fatalf("expected no error on a completed turn, got %v", completed.Error)
	}
}

func TestExecutionTurnFailedSetsErrorAndTimestamp(t *testing.T) {
	turn := NewExecutionTurn(NewExecutionID(), 0, TurnToolCall, "current_time({})")
	failed := turn.Failed("boom")
	if failed.Error == nil || *failed.Error != "boom" {
		t.Fatalf("expected error to be set, got %+v", failed.Error)
	}
	if failed.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set even on failure")
	}
}
