package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type executorFileRepoFake struct {
	files map[uuid.UUID]domain.ProjectFile
}

func (r *executorFileRepoFake) Insert(_ context.Context, f domain.ProjectFile) error {
	r.files[f.ID] = f
	return nil
}
func (r *executorFileRepoFake) FindByID(_ context.Context, id uuid.UUID) (domain.ProjectFile, error) {
	f, ok := r.files[id]
	if !ok {
		return domain.ProjectFile{}, ErrProjectFileNotFound
	}
	return f, nil
}
func (r *executorFileRepoFake) ListByProject(context.Context, domain.ProjectID) ([]domain.ProjectFile, error) {
	return nil, nil
}
func (r *executorFileRepoFake) SoftDelete(context.Context, uuid.UUID) error { return nil }

type executorBlobFake struct {
	data  map[uuid.UUID][]byte
	saves int
}

func (b *executorBlobFake) Save(id uuid.UUID, src io.Reader, max int64) (SavedBlob, error) {
	data, err := io.ReadAll(io.LimitReader(src, max+1))
	if err != nil {
		return SavedBlob{}, err
	}
	if int64(len(data)) > max {
		return SavedBlob{TooLarge: true}, nil
	}
	if _, ok := b.data[id]; ok {
		return SavedBlob{}, errors.New("blob exists")
	}
	b.data[id] = data
	b.saves++
	sum := sha256.Sum256(data)
	return SavedBlob{Size: int64(len(data)), SHA256: sum[:], Head: data}, nil
}
func (b *executorBlobFake) Open(id uuid.UUID, _ domain.ProjectFile) (io.Reader, io.Closer, error) {
	return bytes.NewReader(b.data[id]), io.NopCloser(strings.NewReader("")), nil
}
func (b *executorBlobFake) Remove(id uuid.UUID) error { delete(b.data, id); return nil }

func TestExecutorFileUploadIsExactAndIdempotent(t *testing.T) {
	projectID, taskID, executionID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().Add(time.Minute)
	req := ExecutorRequest{ID: uuid.New(), Status: "APPROVED", StartBefore: &now, Payload: ExecutorPayload{
		ProjectID: projectID.String(), TaskID: taskID.String(), ExecutionID: executionID.String(), Operation: "CREATE", Args: ExecutorArgs{Path: "pasta/nota.txt", ContentBase64: "bm90YQ=="}}}
	repo := &executorFileRepoFake{files: map[uuid.UUID]domain.ProjectFile{}}
	blobs := &executorBlobFake{data: map[uuid.UUID][]byte{}}
	service := NewProjectFileService(repo, blobs)
	if _, _, err := service.UploadFromExecutor(context.Background(), req, strings.NewReader("wrong")); !errors.Is(err, ErrExecutorConflict) {
		t.Fatal("wrong bytes accepted")
	}
	if len(repo.files) != 0 || len(blobs.data) != 0 {
		t.Fatal("wrong bytes survived")
	}
	file, created, err := service.UploadFromExecutor(context.Background(), req, strings.NewReader("nota"))
	if err != nil || !created {
		t.Fatalf("correct upload failed: %v", err)
	}
	if file.ProjectID.String() != projectID.String() || file.RelativePath == nil || *file.RelativePath != "pasta/nota.txt" || file.OriginTaskID == nil || file.OriginTaskID.String() != taskID.String() {
		t.Fatal("file lost project/task/path")
	}
	other, created, err := service.UploadFromExecutor(context.Background(), req, strings.NewReader("nota"))
	if err != nil || created || other.ID != file.ID || blobs.saves != 2 {
		t.Fatalf("retry duplicated file: %v", err)
	}
	if ExecutorFileID(req.ID, req.Payload.Args.Path) != file.ID {
		t.Fatal("file ID not stable")
	}
}
