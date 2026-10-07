package dispatch

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrUnauthorized = errors.New("workspace start is not authorized")
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Assignment comes from a future atomic queue claim. Grant is fetched separately from the
// Prelo authority immediately before touching the runtime; a queue message alone is never an
// approval. The endpoint is deliberately not guessed or called until its contract is frozen.
type Assignment struct {
	JobID       string
	ProjectID   string
	WorkerID    string
	ExecutionID string
	ImageDigest string
	PayloadHash string
	Operation   string
}
type Grant struct {
	JobID       string
	ProjectID   string
	WorkerID    string
	ExecutionID string
	ImageDigest string
	PayloadHash string
	Status      string
	Operation   string
	StartBefore time.Time
}
type GrantReader interface {
	Current(context.Context, string) (Grant, error)
}
type Runtime interface {
	Start(context.Context, string) error
}

type Starter struct {
	authority   GrantReader
	runtime     Runtime
	workerID    string
	projectID   string
	imageDigest string
	now         func() time.Time
}

func NewStarter(authority GrantReader, runtime Runtime, workerID, projectID, imageDigest string) *Starter {
	return &Starter{authority: authority, runtime: runtime, workerID: workerID, projectID: projectID, imageDigest: imageDigest, now: time.Now}
}

func (s *Starter) Start(ctx context.Context, job Assignment) error {
	if job.Operation != "START_WORKSPACE" {
		return ErrUnauthorized
	}
	if err := s.Verify(ctx, job); err != nil {
		return err
	}
	return s.runtime.Start(ctx, job.ExecutionID)
}

func (s *Starter) Verify(ctx context.Context, job Assignment) error {
	if s.authority == nil || s.runtime == nil || s.workerID == "" || s.projectID == "" || job.JobID == "" || job.ProjectID != s.projectID || job.WorkerID != s.workerID || job.ExecutionID == "" || !hashPattern.MatchString(job.PayloadHash) || job.ImageDigest != s.imageDigest {
		return ErrUnauthorized
	}
	grant, err := s.authority.Current(ctx, job.JobID)
	if err != nil {
		return err
	} // network/parsing failure never reaches the runtime
	if grant.Status != "APPROVED" || grant.Operation != job.Operation ||
		grant.JobID != job.JobID || grant.ProjectID != job.ProjectID || grant.WorkerID != job.WorkerID ||
		grant.ExecutionID != job.ExecutionID || grant.ImageDigest != job.ImageDigest || grant.PayloadHash != job.PayloadHash ||
		grant.StartBefore.IsZero() || !s.now().Before(grant.StartBefore) {
		return ErrUnauthorized
	}
	return nil
}
