package domain

import (
	"strings"
	"testing"
)

func TestNewManualContextItem_Limits(t *testing.T) {
	if _, err := NewManualContextItem("", "content"); err == nil {
		t.Fatal("expected error for blank name")
	}
	if _, err := NewManualContextItem(strings.Repeat("a", MaxManualNameLength+1), "content"); err == nil {
		t.Fatal("expected error for oversized name")
	}
	if _, err := NewManualContextItem("name", ""); err == nil {
		t.Fatal("expected error for blank content")
	}
	if _, err := NewManualContextItem("name", strings.Repeat("a", MaxManualContentLength+1)); err == nil {
		t.Fatal("expected error for oversized content")
	}
	if _, err := NewManualContextItem("name", "content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveContextSnapshot_RejectsEmpty(t *testing.T) {
	if _, err := ResolveContextSnapshot(NewTaskID(), 1, nil); err == nil {
		t.Fatal("expected error for empty items")
	}
}

func TestResolveContextSnapshot_RejectsTooManyItems(t *testing.T) {
	items := make([]ContextSnapshotItem, ContextSnapshotMaxItems+1)
	for i := range items {
		items[i], _ = NewContextSnapshotItem("name", "c", ContextSourceManual, "manual-input", i)
	}
	if _, err := ResolveContextSnapshot(NewTaskID(), 1, items); err == nil {
		t.Fatal("expected error for too many items")
	}
}

func TestResolveContextSnapshot_RejectsAggregateOverBudget(t *testing.T) {
	big, _ := NewContextSnapshotItem("name", strings.Repeat("a", MaxManualContentLength), ContextSourceManual, "manual-input", 0)
	items := []ContextSnapshotItem{big, big, big, big}
	if _, err := ResolveContextSnapshot(NewTaskID(), 1, items); err == nil {
		t.Fatal("expected error for aggregate content over budget")
	}
}

func TestResolveContextSnapshot_Valid(t *testing.T) {
	item, err := NewContextSnapshotItem("description", "do the thing", ContextSourceManual, "task-description", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snapshot, err := ResolveContextSnapshot(NewTaskID(), 1, []ContextSnapshotItem{item})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshot.Items) != 1 || snapshot.Version != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}
