package application

import (
	"context"

	"github.com/exotermo/prelo-core/internal/domain"
)

// ToolAvailability is a server-owned project ceiling over the boot-curated agent catalog.
// An absent setting means the legacy catalog remains available; it never adds a capability.
type ToolAvailability interface {
	Allowed(ctx context.Context, projectID domain.ProjectID, toolName string) (bool, error)
	Get(ctx context.Context, projectID domain.ProjectID, toolName string) (ToolSetting, error)
}

type ToolSetting struct {
	Enabled bool
	Version int64
}

type ToolSettings interface {
	ToolAvailability
	Get(ctx context.Context, projectID domain.ProjectID, toolName string) (ToolSetting, error)
	Set(ctx context.Context, projectID domain.ProjectID, toolName string, enabled bool, expectedVersion int64, actor string) (ToolSetting, error)
}
