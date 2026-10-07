package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

var ErrProjectFileNotFound = errors.New("project file not found")
var ErrFileTooLarge = errors.New("file exceeds the 100 MB limit")

type ProjectFileRepository interface {
	Insert(ctx context.Context, file domain.ProjectFile) error
	FindByID(ctx context.Context, id uuid.UUID) (domain.ProjectFile, error)
	ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ProjectFile, error)
	SoftDelete(ctx context.Context, id uuid.UUID) error
}

// FileBlobs is the narrow port over filestore.Store: sealed bytes on disk. Size/hash/head come
// back from Save so the service never has to buffer or re-read an upload.
type FileBlobs interface {
	Save(id uuid.UUID, src io.Reader, maxBytes int64) (SavedBlob, error)
	Open(id uuid.UUID, file domain.ProjectFile) (io.Reader, io.Closer, error)
	Remove(id uuid.UUID) error
}

type SavedBlob struct {
	Salt, NoncePrefix []byte
	ChunkSize         int
	Size              int64
	SHA256, Head      []byte
	TooLarge          bool
}

// ProjectFileService (Fase PA): upload, list, read and remove a project's files. Membership is
// enforced by the HTTP layer; this service only guarantees a file is never reachable through a
// different project than its own.
type ProjectFileService struct {
	files ProjectFileRepository
	blobs FileBlobs
}

func NewProjectFileService(files ProjectFileRepository, blobs FileBlobs) *ProjectFileService {
	return &ProjectFileService{files: files, blobs: blobs}
}

func (s *ProjectFileService) Upload(ctx context.Context, projectID domain.ProjectID, name string, src io.Reader, uploadedBy string) (domain.ProjectFile, error) {
	id := uuid.New()
	saved, err := s.blobs.Save(id, src, domain.MaxProjectFileBytes)
	if err != nil {
		return domain.ProjectFile{}, err
	}
	if saved.TooLarge {
		return domain.ProjectFile{}, ErrFileTooLarge
	}
	cleanName := domain.SanitizeFileName(name)
	contentType, kind := domain.ClassifyFile(cleanName, http.DetectContentType(saved.Head))
	file := domain.ProjectFile{
		ID: id, ProjectID: projectID, Name: cleanName, ContentType: contentType, Kind: kind,
		SizeBytes: saved.Size, SHA256: saved.SHA256, Salt: saved.Salt, NoncePrefix: saved.NoncePrefix,
		ChunkSize: saved.ChunkSize, UploadedBy: uploadedBy,
	}
	if err := s.files.Insert(ctx, file); err != nil {
		_ = s.blobs.Remove(id)
		return domain.ProjectFile{}, err
	}
	return s.files.FindByID(ctx, id)
}

// ExecutorFileID is stable across retries and is derived on the server, never chosen by the
// worker. A second upload cannot replace the sealed blob associated with this ID.
func ExecutorFileID(requestID uuid.UUID, relativePath string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("prelo-executor-file:"+requestID.String()+":"+relativePath))
}

func (s *ProjectFileService) UploadFromExecutor(ctx context.Context, request ExecutorRequest, src io.Reader) (domain.ProjectFile, bool, error) {
	if request.Status != "APPROVED" || request.Payload.Operation != "CREATE" || request.StartBefore == nil {
		return domain.ProjectFile{}, false, ErrExecutorUnauthorized
	}
	relativePath := request.Payload.Args.Path
	if err := ValidateExecutorPath(relativePath, false); err != nil {
		return domain.ProjectFile{}, false, err
	}
	expected, err := base64.StdEncoding.DecodeString(request.Payload.Args.ContentBase64)
	if err != nil || len(expected) > 64<<10 {
		return domain.ProjectFile{}, false, ErrExecutorUnauthorized
	}
	hash := sha256.Sum256(expected)
	id := ExecutorFileID(request.ID, relativePath)
	projectUUID, _ := uuid.Parse(request.Payload.ProjectID)
	projectID := domain.ProjectID{Value: projectUUID}
	if existing, err := s.files.FindByID(ctx, id); err == nil {
		if existing.ProjectID == projectID && existing.ExecutorRequestID != nil && *existing.ExecutorRequestID == request.ID && existing.RelativePath != nil && *existing.RelativePath == relativePath && existing.SizeBytes == int64(len(expected)) && string(existing.SHA256) == string(hash[:]) && existing.DeletedAt == nil {
			return existing, false, nil
		}
		return domain.ProjectFile{}, false, ErrExecutorConflict
	} else if !errors.Is(err, ErrProjectFileNotFound) {
		return domain.ProjectFile{}, false, err
	}
	saved, err := s.blobs.Save(id, src, 64<<10)
	if err != nil {
		return domain.ProjectFile{}, false, err
	}
	if saved.TooLarge {
		return domain.ProjectFile{}, false, ErrFileTooLarge
	}
	if saved.Size != int64(len(expected)) || string(saved.SHA256) != string(hash[:]) {
		_ = s.blobs.Remove(id)
		return domain.ProjectFile{}, false, ErrExecutorConflict
	}
	contentType, kind := domain.ClassifyFile(path.Base(relativePath), http.DetectContentType(saved.Head))
	taskUUID, _ := uuid.Parse(request.Payload.TaskID)
	executionUUID, _ := uuid.Parse(request.Payload.ExecutionID)
	taskID := domain.TaskID{Value: taskUUID}
	executionID := domain.ExecutionID{Value: executionUUID}
	file := domain.ProjectFile{ID: id, ProjectID: projectID, Name: domain.SanitizeFileName(path.Base(relativePath)), RelativePath: &relativePath,
		ContentType: contentType, Kind: kind, SizeBytes: saved.Size, SHA256: saved.SHA256, Salt: saved.Salt, NoncePrefix: saved.NoncePrefix, ChunkSize: saved.ChunkSize,
		UploadedBy: "prelo:task:" + taskID.String(), OriginTaskID: &taskID, OriginExecutionID: &executionID, ExecutorRequestID: &request.ID}
	if err := s.files.Insert(ctx, file); err != nil {
		_ = s.blobs.Remove(id)
		return domain.ProjectFile{}, false, err
	}
	stored, err := s.files.FindByID(ctx, id)
	return stored, true, err
}

func (s *ProjectFileService) List(ctx context.Context, projectID domain.ProjectID) ([]domain.ProjectFile, error) {
	return s.files.ListByProject(ctx, projectID)
}

func (s *ProjectFileService) find(ctx context.Context, projectID domain.ProjectID, id uuid.UUID) (domain.ProjectFile, error) {
	file, err := s.files.FindByID(ctx, id)
	if err != nil {
		return domain.ProjectFile{}, err
	}
	if file.ProjectID != projectID || file.DeletedAt != nil {
		return domain.ProjectFile{}, ErrProjectFileNotFound
	}
	return file, nil
}

func (s *ProjectFileService) Open(ctx context.Context, projectID domain.ProjectID, id uuid.UUID) (domain.ProjectFile, io.Reader, io.Closer, error) {
	file, err := s.find(ctx, projectID, id)
	if err != nil {
		return domain.ProjectFile{}, nil, nil, err
	}
	r, closer, err := s.blobs.Open(id, file)
	if err != nil {
		return domain.ProjectFile{}, nil, nil, err
	}
	return file, r, closer, nil
}

// Delete removes a file; allowed for a project manager or whoever uploaded it.
func (s *ProjectFileService) Delete(ctx context.Context, projectID domain.ProjectID, id uuid.UUID, caller string, canManage bool) error {
	file, err := s.find(ctx, projectID, id)
	if err != nil {
		return err
	}
	if !canManage && file.UploadedBy != caller {
		return ErrForbidden
	}
	if err := s.files.SoftDelete(ctx, id); err != nil {
		return err
	}
	return s.blobs.Remove(id)
}

// PurgeProject removes every file of a deleted project from disk (metadata stays, soft-deleted).
func (s *ProjectFileService) PurgeProject(ctx context.Context, projectID domain.ProjectID) error {
	files, err := s.files.ListByProject(ctx, projectID)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := s.files.SoftDelete(ctx, f.ID); err != nil {
			return err
		}
		if err := s.blobs.Remove(f.ID); err != nil {
			return err
		}
	}
	return nil
}

var ErrForbidden = errors.New("forbidden")
