package domain

import "testing"

func TestNewSubtaskSetsParentAndDepth(t *testing.T) {
	agentID, _ := NewAgentID("general")
	parent, _ := NewTask("parent task", agentID)

	child, err := NewSubtask("child task", agentID, parent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if child.ParentTaskID == nil || *child.ParentTaskID != parent.ID {
		t.Fatalf("expected child to point at parent, got %+v", child.ParentTaskID)
	}
	if child.Depth != 1 {
		t.Fatalf("expected depth 1, got %d", child.Depth)
	}
}

func TestNewSubtaskEnforcesMaxDelegationDepth(t *testing.T) {
	agentID, _ := NewAgentID("general")
	task, _ := NewTask("root", agentID)
	for i := 0; i < MaxDelegationDepth; i++ {
		next, err := NewSubtask("nested", agentID, task)
		if err != nil {
			t.Fatalf("unexpected error at depth %d: %v", i+1, err)
		}
		task = next
	}
	if task.Depth != MaxDelegationDepth {
		t.Fatalf("expected to reach max depth %d, got %d", MaxDelegationDepth, task.Depth)
	}
	if _, err := NewSubtask("one too many", agentID, task); err == nil {
		t.Fatal("expected delegating beyond MaxDelegationDepth to fail")
	}
}
