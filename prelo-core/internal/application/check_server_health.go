package application

import (
	"context"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// CheckServerHealthUseCase runs one on-demand health check (no background polling in Fase S1 —
// the dashboard's "Atualizar" button drives this) and persists the outcome either way, so a
// server that's gone offline shows that on the next List() instead of its last-known-good status.
type CheckServerHealthUseCase struct {
	servers ServerRepository
	checker ServerHealthChecker
	events  EventPublisher
}

func NewCheckServerHealthUseCase(servers ServerRepository, checker ServerHealthChecker) *CheckServerHealthUseCase {
	return &CheckServerHealthUseCase{servers: servers, checker: checker, events: NoopEventPublisher{}}
}

// SetEventPublisher wires Fase I's server.offline webhook event.
func (uc *CheckServerHealthUseCase) SetEventPublisher(events EventPublisher) { uc.events = events }

func isDown(status domain.ServerStatus) bool {
	return status == domain.ServerStatusOffline || status == domain.ServerStatusError
}

func (uc *CheckServerHealthUseCase) Check(ctx context.Context, id domain.ServerID) (ServerHealthSnapshot, error) {
	server, err := uc.servers.FindByID(ctx, id)
	if err != nil {
		return ServerHealthSnapshot{}, err
	}

	snapshot, err := uc.checker.Check(ctx, server)
	if err != nil {
		return ServerHealthSnapshot{}, err
	}

	var errMessage *string
	if snapshot.Error != "" {
		errMessage = &snapshot.Error
	}
	previous := server.LastStatus
	server = server.WithHealthResult(snapshot.Status, time.Now().UTC(), errMessage)
	if _, err := uc.servers.Update(ctx, server); err != nil {
		return ServerHealthSnapshot{}, err
	}
	// Only the transition into "down" is an event — re-checking a server that was already down
	// must not re-notify on every click.
	if isDown(snapshot.Status) && !isDown(previous) {
		uc.events.Publish(ctx, server.ProjectID, domain.EventServerOffline, map[string]any{
			"serverId": server.ID.String(), "name": server.Name, "host": server.Host,
			"status": string(snapshot.Status), "error": snapshot.Error,
		})
	}
	return snapshot, nil
}
