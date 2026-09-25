package application

import (
	"context"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

// ContextResolver builds a ContextSnapshot from manual sources only (no automatic memory, no
// RAG, no embeddings in this iteration). It never persists anything itself — the caller
// decides when to save the resulting snapshot via ContextSnapshotRepository.
type ContextResolver struct {
	manualContext ManualContextRepository
	snapshots     ContextSnapshotRepository
}

func NewContextResolver(manualContext ManualContextRepository, snapshots ContextSnapshotRepository) *ContextResolver {
	return &ContextResolver{manualContext: manualContext, snapshots: snapshots}
}

func (r *ContextResolver) Resolve(ctx context.Context, task domain.Task) (domain.ContextSnapshot, error) {
	descriptionItem, err := domain.NewContextSnapshotItem("description", task.Description, domain.ContextSourceManual, "task-description", 0)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	items := []domain.ContextSnapshotItem{descriptionItem}

	manualItems, err := r.manualContext.FindByTaskID(ctx, task.ID)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	order := 1
	for _, item := range manualItems {
		snapshotItem, err := domain.NewContextSnapshotItem(item.Name, item.Content, domain.ContextSourceManual, "manual-input", order)
		if err != nil {
			return domain.ContextSnapshot{}, err
		}
		items = append(items, snapshotItem)
		order++
	}

	nextVersion := 1
	latest, found, err := r.snapshots.FindLatestByTaskID(ctx, task.ID)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	if found {
		nextVersion = latest.Version + 1
	}

	return domain.ResolveContextSnapshot(task.ID, nextVersion, items)
}
