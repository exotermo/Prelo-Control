package domain

import "testing"

func TestExecution_Lifecycle(t *testing.T) {
	taskID := NewTaskID()
	agentID := mustAgentID(t, "general")

	exec := NewPendingExecution(taskID, agentID)
	if exec.Status != ExecutionPending {
		t.Fatalf("expected PENDING, got %s", exec.Status)
	}

	running := exec.Running()
	if running.Status != ExecutionRunning || running.StartedAt == nil {
		t.Fatalf("expected RUNNING with StartedAt set, got %+v", running)
	}

	snapshotID := NewContextSnapshotID()
	withSnapshot := running.WithContextSnapshotID(snapshotID)
	if withSnapshot.ContextSnapshotID == nil || *withSnapshot.ContextSnapshotID != snapshotID {
		t.Fatalf("expected context snapshot id attached")
	}

	completed := withSnapshot.Completed("hello", "req-1", "claude-x", "anthropic")
	if completed.Status != ExecutionCompleted || completed.CompletedAt == nil {
		t.Fatalf("expected COMPLETED with CompletedAt set, got %+v", completed)
	}
	if completed.ContextSnapshotID == nil || *completed.ContextSnapshotID != snapshotID {
		t.Fatal("expected context snapshot id to survive completion")
	}
	if *completed.Result != "hello" || *completed.RequestID != "req-1" || *completed.Model != "claude-x" || *completed.Provider != "anthropic" {
		t.Fatalf("unexpected completed fields: %+v", completed)
	}
}

func TestExecution_FailedPreservesContextSnapshot(t *testing.T) {
	taskID := NewTaskID()
	agentID := mustAgentID(t, "general")
	snapshotID := NewContextSnapshotID()

	exec := NewPendingExecution(taskID, agentID).Running().WithContextSnapshotID(snapshotID)
	failed := exec.Failed("gateway timeout")

	if failed.Status != ExecutionFailed {
		t.Fatalf("expected FAILED, got %s", failed.Status)
	}
	if failed.Error == nil || *failed.Error != "gateway timeout" {
		t.Fatalf("expected error message set, got %+v", failed.Error)
	}
	if failed.ContextSnapshotID == nil || *failed.ContextSnapshotID != snapshotID {
		t.Fatal("expected context snapshot id preserved on failure")
	}
}

func TestExecution_WithHelpers(t *testing.T) {
	exec := NewPendingExecution(NewTaskID(), mustAgentID(t, "general"))

	versioned := exec.WithVersion(3)
	if versioned.Version != 3 {
		t.Fatalf("expected version 3, got %d", versioned.Version)
	}

	withAgentVersion := exec.WithAgentVersion("1.1")
	if withAgentVersion.AgentVersion == nil || *withAgentVersion.AgentVersion != "1.1" {
		t.Fatal("expected agent version set")
	}

	withRequestID := exec.WithRequestID("req-99")
	if withRequestID.RequestID == nil || *withRequestID.RequestID != "req-99" {
		t.Fatal("expected request id set")
	}
}
