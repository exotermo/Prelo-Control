package domain

import "time"

type ContextSourceType string

const ContextSourceManual ContextSourceType = "MANUAL"

// ManualContextItem is a caller-submitted context item attached to a task at creation time,
// before it's frozen into a ContextSnapshotItem by ContextResolver. Kept distinct from
// ContextSnapshotItem because it carries no order/provenance/source-type yet.
type ManualContextItem struct {
	Name    string
	Content string
}

// MaxManualNameLength matches `task_manual_context_items.name VARCHAR(200)` (V5 migration).
const MaxManualNameLength = 200

// MaxManualContentLength has no DB-level cap (content is TEXT) but protects the compiled
// prompt budget — see ContextSnapshotMaxAggregateContentLength. Never rely on the Gateway
// alone to reject an oversized item coming from Prelo.
const MaxManualContentLength = 8000

func NewManualContextItem(name, content string) (ManualContextItem, error) {
	if isBlank(name) {
		return ManualContextItem{}, &ValidationError{Message: "context item name is required"}
	}
	if len(name) > MaxManualNameLength {
		return ManualContextItem{}, &ValidationError{Message: "context item name must be at most 200 characters"}
	}
	if isBlank(content) {
		return ManualContextItem{}, &ValidationError{Message: "context item content is required"}
	}
	if len(content) > MaxManualContentLength {
		return ManualContextItem{}, &ValidationError{Message: "context item content must be at most 8000 characters"}
	}
	return ManualContextItem{Name: name, Content: content}, nil
}

type ContextSnapshotItem struct {
	Name       string
	Content    string
	SourceType ContextSourceType
	Provenance string
	Order      int
}

func NewContextSnapshotItem(name, content string, sourceType ContextSourceType, provenance string, order int) (ContextSnapshotItem, error) {
	if isBlank(name) {
		return ContextSnapshotItem{}, &ValidationError{Message: "context item name is required"}
	}
	if isBlank(content) {
		return ContextSnapshotItem{}, &ValidationError{Message: "context item content is required"}
	}
	if len(content) > MaxManualContentLength {
		return ContextSnapshotItem{}, &ValidationError{Message: "context item content must be at most 8000 characters"}
	}
	return ContextSnapshotItem{Name: name, Content: content, SourceType: sourceType, Provenance: provenance, Order: order}, nil
}

// ContextSnapshotMaxItems and ContextSnapshotMaxAggregateContentLength give a curated,
// explicit budget so a compiled prompt can never silently exceed what the Gateway accepts
// per message — checked here, at the domain boundary, not just relied upon from the
// Gateway's own validation.
const (
	ContextSnapshotMaxItems                  = 20
	ContextSnapshotMaxAggregateContentLength = 24_000
)

type ContextSnapshot struct {
	ID         ContextSnapshotID
	TaskID     TaskID
	Version    int
	ResolvedAt time.Time
	Items      []ContextSnapshotItem
}

func ResolveContextSnapshot(taskID TaskID, version int, items []ContextSnapshotItem) (ContextSnapshot, error) {
	if len(items) == 0 {
		return ContextSnapshot{}, &ValidationError{Message: "a context snapshot must have at least one item"}
	}
	if len(items) > ContextSnapshotMaxItems {
		return ContextSnapshot{}, &ValidationError{Message: "a context snapshot must have at most 20 items"}
	}
	aggregate := 0
	for _, item := range items {
		aggregate += len(item.Content)
	}
	if aggregate > ContextSnapshotMaxAggregateContentLength {
		return ContextSnapshot{}, &ValidationError{Message: "context snapshot content totals over the 24000 character budget"}
	}
	return ContextSnapshot{
		ID:         NewContextSnapshotID(),
		TaskID:     taskID,
		Version:    version,
		ResolvedAt: time.Now().UTC(),
		Items:      items,
	}, nil
}
