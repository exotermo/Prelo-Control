package domain

import (
	"errors"
	"strings"
	"testing"
)

func mustAgentID(t *testing.T, value string) AgentID {
	t.Helper()
	id, err := NewAgentID(value)
	if err != nil {
		t.Fatalf("unexpected error building agent id: %v", err)
	}
	return id
}

func TestNewTask_Valid(t *testing.T) {
	task, err := NewTask("do something", mustAgentID(t, "general"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != TaskCreated {
		t.Fatalf("expected CREATED, got %s", task.Status)
	}
	if task.Version != 0 {
		t.Fatalf("expected version 0, got %d", task.Version)
	}
}

func TestNewTask_RejectsBlankDescription(t *testing.T) {
	if _, err := NewTask("   ", mustAgentID(t, "general")); err == nil {
		t.Fatal("expected error for blank description")
	}
}

func TestNewTask_RejectsOversizedDescription(t *testing.T) {
	oversized := strings.Repeat("a", MaxDescriptionLength+1)
	if _, err := NewTask(oversized, mustAgentID(t, "general")); err == nil {
		t.Fatal("expected error for oversized description")
	}
}

func TestTask_QueuedFromCreated(t *testing.T) {
	task, _ := NewTask("desc", mustAgentID(t, "general"))
	queued, err := task.Queued()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queued.Status != TaskQueued {
		t.Fatalf("expected QUEUED, got %s", queued.Status)
	}
}

func TestTask_QueuedRejectsSecondEnqueue(t *testing.T) {
	task, _ := NewTask("desc", mustAgentID(t, "general"))
	queued, _ := task.Queued()
	if _, err := queued.Queued(); !isInvalidTransition(err) {
		t.Fatalf("expected invalid transition re-queuing an already-queued task, got %v", err)
	}
}

func TestTask_RunningFromCreatedOrQueued(t *testing.T) {
	for _, status := range []TaskStatus{TaskCreated, TaskQueued} {
		task, _ := NewTask("desc", mustAgentID(t, "general"))
		task.Status = status
		running, err := task.Running()
		if err != nil {
			t.Fatalf("unexpected error transitioning from %s: %v", status, err)
		}
		if running.Status != TaskRunning {
			t.Fatalf("expected RUNNING, got %s", running.Status)
		}
	}
}

func TestTask_RunningRejectsFromTerminalStates(t *testing.T) {
	for _, status := range []TaskStatus{TaskRunning, TaskCompleted, TaskFailed} {
		task, _ := NewTask("desc", mustAgentID(t, "general"))
		task.Status = status
		if _, err := task.Running(); !isInvalidTransition(err) {
			t.Fatalf("expected invalid transition from %s, got %v", status, err)
		}
	}
}

func TestTask_CompletedAndFailedOnlyFromRunning(t *testing.T) {
	task, _ := NewTask("desc", mustAgentID(t, "general"))
	task.Status = TaskRunning

	completed, err := task.Completed()
	if err != nil || completed.Status != TaskCompleted {
		t.Fatalf("expected COMPLETED, got %+v err=%v", completed, err)
	}

	task.Status = TaskRunning
	failed, err := task.Failed()
	if err != nil || failed.Status != TaskFailed {
		t.Fatalf("expected FAILED, got %+v err=%v", failed, err)
	}

	for _, status := range []TaskStatus{TaskCreated, TaskQueued, TaskCompleted, TaskFailed} {
		task.Status = status
		if _, err := task.Completed(); !isInvalidTransition(err) {
			t.Fatalf("expected invalid transition to COMPLETED from %s, got %v", status, err)
		}
		if _, err := task.Failed(); !isInvalidTransition(err) {
			t.Fatalf("expected invalid transition to FAILED from %s, got %v", status, err)
		}
	}
}

func TestTask_WithVersion(t *testing.T) {
	task, _ := NewTask("desc", mustAgentID(t, "general"))
	updated := task.WithVersion(5)
	if updated.Version != 5 {
		t.Fatalf("expected version 5, got %d", updated.Version)
	}
}

func isInvalidTransition(err error) bool {
	var target *InvalidTransitionError
	return errors.As(err, &target)
}
