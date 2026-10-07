package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

var ErrExecutorWorkerNotFound = errors.New("executor worker not found")
var ErrExecutorWorkerUnauthorized = errors.New("executor worker unauthorized")

var imageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ExecutorWorker struct {
	ID          uuid.UUID
	Name        string
	ProjectID   domain.ProjectID
	ImageDigest string
	TokenHash   []byte
	Enabled     bool
	CreatedBy   string
	CreatedAt   time.Time
	LastSeenAt  *time.Time
}

type ExecutorWorkerRepository interface {
	Insert(ctx context.Context, worker ExecutorWorker) error
	FindByID(ctx context.Context, id uuid.UUID) (ExecutorWorker, error)
	FindByTokenHash(ctx context.Context, hash []byte) (ExecutorWorker, error)
	List(ctx context.Context) ([]ExecutorWorker, error)
	Touch(ctx context.Context, id uuid.UUID) error
	Revoke(ctx context.Context, id uuid.UUID) error
}

type ExecutorWorkerService struct{ workers ExecutorWorkerRepository }

func NewExecutorWorkerService(workers ExecutorWorkerRepository) *ExecutorWorkerService {
	return &ExecutorWorkerService{workers: workers}
}

// Register is invoked only from a human ADMIN handler. It returns the credential once;
// persistence contains only its SHA-256 hash. The credential is scoped to heartbeat only.
func (s *ExecutorWorkerService) Register(ctx context.Context, name string, projectID domain.ProjectID, imageDigest, actor string) (ExecutorWorker, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 || projectID.Value == uuid.Nil || !imageDigestPattern.MatchString(imageDigest) || strings.TrimSpace(actor) == "" {
		return ExecutorWorker{}, "", &domain.ValidationError{Message: "name, projectId, imageDigest and administrator are required"}
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return ExecutorWorker{}, "", err
	}
	token := "prw_" + base64.RawURLEncoding.EncodeToString(secret[:])
	hash := sha256.Sum256([]byte(token))
	worker := ExecutorWorker{ID: uuid.New(), Name: name, ProjectID: projectID, ImageDigest: imageDigest,
		TokenHash: hash[:], Enabled: true, CreatedBy: actor, CreatedAt: time.Now().UTC()}
	if err := s.workers.Insert(ctx, worker); err != nil {
		return ExecutorWorker{}, "", err
	}
	worker.TokenHash = nil // never return even the hash to callers
	return worker, token, nil
}

func (s *ExecutorWorkerService) Authenticate(ctx context.Context, token string) (ExecutorWorker, error) {
	if !strings.HasPrefix(token, "prw_") || len(token) != 47 {
		return ExecutorWorker{}, ErrExecutorWorkerUnauthorized
	}
	hash := sha256.Sum256([]byte(token))
	worker, err := s.workers.FindByTokenHash(ctx, hash[:])
	if err != nil {
		if errors.Is(err, ErrExecutorWorkerNotFound) {
			return ExecutorWorker{}, ErrExecutorWorkerUnauthorized
		}
		return ExecutorWorker{}, err
	}
	if !worker.Enabled {
		return ExecutorWorker{}, ErrExecutorWorkerUnauthorized
	}
	worker.TokenHash = nil
	return worker, nil
}

func (s *ExecutorWorkerService) Heartbeat(ctx context.Context, token string) (ExecutorWorker, error) {
	worker, err := s.Authenticate(ctx, token)
	if err != nil {
		return ExecutorWorker{}, err
	}
	if err := s.workers.Touch(ctx, worker.ID); err != nil {
		return ExecutorWorker{}, err
	}
	now := time.Now().UTC()
	worker.LastSeenAt = &now
	return worker, nil
}

func (s *ExecutorWorkerService) List(ctx context.Context) ([]ExecutorWorker, error) {
	workers, err := s.workers.List(ctx)
	for i := range workers {
		workers[i].TokenHash = nil
	}
	return workers, err
}

func (s *ExecutorWorkerService) Revoke(ctx context.Context, id uuid.UUID) error {
	return s.workers.Revoke(ctx, id)
}
