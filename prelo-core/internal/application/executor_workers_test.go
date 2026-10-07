package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type workerMemoryRepo struct{ rows map[uuid.UUID]ExecutorWorker }

func (r *workerMemoryRepo) Insert(_ context.Context, w ExecutorWorker) error {
	if r.rows == nil {
		r.rows = map[uuid.UUID]ExecutorWorker{}
	}
	r.rows[w.ID] = w
	return nil
}
func (r *workerMemoryRepo) FindByTokenHash(_ context.Context, hash []byte) (ExecutorWorker, error) {
	for _, w := range r.rows {
		if w.Enabled && string(w.TokenHash) == string(hash) {
			return w, nil
		}
	}
	return ExecutorWorker{}, ErrExecutorWorkerNotFound
}
func (r *workerMemoryRepo) FindByID(_ context.Context,id uuid.UUID) (ExecutorWorker,error) {
	w,ok:=r.rows[id]; if !ok { return ExecutorWorker{},ErrExecutorWorkerNotFound }; return w,nil
}
func (r *workerMemoryRepo) List(context.Context) ([]ExecutorWorker, error) {
	result := make([]ExecutorWorker, 0, len(r.rows))
	for _, w := range r.rows {
		result = append(result, w)
	}
	return result, nil
}
func (r *workerMemoryRepo) Touch(_ context.Context, id uuid.UUID) error {
	w, ok := r.rows[id]
	if !ok || !w.Enabled {
		return ErrExecutorWorkerUnauthorized
	}
	return nil
}
func (r *workerMemoryRepo) Revoke(_ context.Context, id uuid.UUID) error {
	w, ok := r.rows[id]
	if !ok || !w.Enabled {
		return ErrExecutorWorkerNotFound
	}
	w.Enabled = false
	r.rows[id] = w
	return nil
}

func TestExecutorWorkerCredentialIsScopedAndRevocable(t *testing.T) {
	ctx := context.Background()
	repo := &workerMemoryRepo{}
	svc := NewExecutorWorkerService(repo)
	project := domain.ProjectID{Value: uuid.New()}
	w, token, err := svc.Register(ctx, "vm-1", project, "sha256:invalid", "admin")
	if err == nil {
		t.Fatal("accepted invalid digest")
	}
	w, token, err = svc.Register(ctx, "vm-1", project, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 47 || len(w.TokenHash) != 0 {
		t.Fatal("credential leaked into worker view")
	}
	stored := repo.rows[w.ID]
	hash := sha256.Sum256([]byte(token))
	if string(stored.TokenHash) != string(hash[:]) {
		t.Fatal("credential was not stored as a hash")
	}
	auth, err := svc.Authenticate(ctx, token)
	if err != nil || auth.ProjectID != project || len(auth.TokenHash) != 0 {
		t.Fatalf("authentication failed: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token+"x"); !errors.Is(err, ErrExecutorWorkerUnauthorized) {
		t.Fatal("invalid credential accepted")
	}
	if err := svc.Revoke(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Heartbeat(ctx, token); !errors.Is(err, ErrExecutorWorkerUnauthorized) {
		t.Fatal("revoked credential accepted")
	}
}
