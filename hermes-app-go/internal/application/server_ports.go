package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

var ErrServerNotFound = errors.New("server not found")

// ServerRepository persists registered servers (Fase S1). Update is version-checked, same
// `WHERE id=$1 AND server_version=$2` convention as TaskRepository — the caller must already
// have applied a guarded change (WithCredential/WithHealthResult) to the Server value in-process
// before calling it.
type ServerRepository interface {
	Insert(ctx context.Context, server domain.Server) error
	FindByID(ctx context.Context, id domain.ServerID) (domain.Server, error)
	// List excludes soft-deleted servers, newest first.
	List(ctx context.Context) ([]domain.Server, error)
	// ListByProject backs the Fase W project-scoped Servidores page — nil projectID means the
	// "unassigned" bucket (project_id IS NULL).
	ListByProject(ctx context.Context, projectID *uuid.UUID) ([]domain.Server, error)
	Update(ctx context.Context, server domain.Server) (domain.Server, error)
	SoftDelete(ctx context.Context, id domain.ServerID) error
}

// ContainerStatus is one line of `docker ps` output from a health check — no domain invariants,
// same "plain struct in application" convention as HermesExecutionRecord.
type ContainerStatus struct {
	ID     string
	Name   string
	Image  string
	Status string
}

// ServerHealthSnapshot is what one health-check dial produces. Error is set (and Status is
// ServerStatusOffline/ServerStatusError) instead of the Check call itself returning a Go error
// for anything that isn't a programming/config mistake — a server being unreachable is an
// expected, displayable outcome, not an exceptional one.
type ServerHealthSnapshot struct {
	Status          domain.ServerStatus
	Uptime          string
	MemoryUsedMB    int
	MemoryTotalMB   int
	DiskUsedPercent int
	Containers      []ContainerStatus
	Error           string
}

// ServerHealthChecker is the narrow port RegisterServerUseCase/CheckServerHealthUseCase depend
// on — the concrete SSH implementation lives in infrastructure/ssh, kept out of the application
// layer the same way LanguageModelGateway keeps the Gateway's HTTP details out of ChatService.
type ServerHealthChecker interface {
	Check(ctx context.Context, server domain.Server) (ServerHealthSnapshot, error)
}

// ServerRegistrationDialer is what RegisterServerUseCase uses to validate a brand-new
// credential before anything is persisted — trust-on-first-use: it dials once with no fingerprint
// to compare against yet, captures whatever host key answers, and proves the credential actually
// authenticates (a trivial remote command, not the full health script). Deliberately a separate,
// narrower port from ServerHealthChecker: registration's "accept whatever key shows up this one
// time" behavior must never be reachable from the ordinary health-check path.
type ServerRegistrationDialer interface {
	Register(ctx context.Context, host string, port int, user string, privateKeyPEM []byte) (hostKeyFingerprint string, err error)
}
