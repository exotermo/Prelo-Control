package domain

import "testing"

func TestNewExecutionJob_IDMatchesExecutionID(t *testing.T) {
	taskID := NewTaskID()
	executionID := NewExecutionID()

	job := NewExecutionJob(taskID, executionID)

	if job.ID != executionID || job.ExecutionID != executionID {
		t.Fatalf("expected job.ID and job.ExecutionID to both equal the execution id, got %+v", job)
	}
	if job.Status != JobPending {
		t.Fatalf("expected PENDING, got %s", job.Status)
	}
	if job.MaxAttempts != 5 {
		t.Fatalf("expected default max attempts 5, got %d", job.MaxAttempts)
	}
}
