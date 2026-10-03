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
	projects      ProjectReader
}

// SetProjectReader makes every task of a project carry its instructions (Fase PA) as a context item.
func (r *ContextResolver) SetProjectReader(projects ProjectReader) { r.projects = projects }

func NewContextResolver(manualContext ManualContextRepository, snapshots ContextSnapshotRepository) *ContextResolver {
	return &ContextResolver{manualContext: manualContext, snapshots: snapshots}
}

func (r *ContextResolver) Resolve(ctx context.Context, task domain.Task) (domain.ContextSnapshot, error) {
	descriptionItem, err := domain.NewContextSnapshotItem("description", task.Description, domain.ContextSourceManual, "task-description", 0)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	items := []domain.ContextSnapshotItem{descriptionItem}

	if task.ProjectID != nil && r.projects != nil {
		if project, err := r.projects.FindByID(ctx, *task.ProjectID); err == nil && project.Instructions != nil {
			item, err := domain.NewContextSnapshotItem("instruções do projeto", *project.Instructions, domain.ContextSourceManual, "project-instructions", len(items))
			if err != nil {
				return domain.ContextSnapshot{}, err
			}
			items = append(items, item)
		}
	}

	manualItems, err := r.manualContext.FindByTaskID(ctx, task.ID)
	if err != nil {
		return domain.ContextSnapshot{}, err
	}
	order := len(items)
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
