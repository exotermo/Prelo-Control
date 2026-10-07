package api

import (
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ProjectFileHandler (Fase PA): members of a project list, upload and read its files; removing
// one needs projects:manage or being the uploader. Files are sealed at rest (filestore).
type ProjectFileHandler struct {
	files    *application.ProjectFileService
	projects projectFinder
	members  projectMembershipChecker
}

func NewProjectFileHandler(files *application.ProjectFileService, projects projectFinder, members projectMembershipChecker) *ProjectFileHandler {
	return &ProjectFileHandler{files: files, projects: projects, members: members}
}

type projectFileResponse struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	ContentType       string  `json:"contentType"`
	Kind              string  `json:"kind"`
	Inline            bool    `json:"inline"`
	SizeBytes         int64   `json:"sizeBytes"`
	SHA256            string  `json:"sha256"`
	UploadedBy        string  `json:"uploadedBy"`
	CreatedAt         string  `json:"createdAt"`
	RelativePath      *string `json:"relativePath,omitempty"`
	OriginTaskID      *string `json:"originTaskId,omitempty"`
	OriginExecutionID *string `json:"originExecutionId,omitempty"`
}

func projectFileResponseFrom(f domain.ProjectFile) projectFileResponse {
	v := projectFileResponse{ID: f.ID.String(), Name: f.Name, ContentType: f.ContentType, Kind: f.Kind, Inline: f.Inline(),
		SizeBytes: f.SizeBytes, SHA256: hex.EncodeToString(f.SHA256), UploadedBy: f.UploadedBy, CreatedAt: f.CreatedAt.Format(time.RFC3339), RelativePath: f.RelativePath}
	if f.OriginTaskID != nil {
		s := f.OriginTaskID.String()
		v.OriginTaskID = &s
	}
	if f.OriginExecutionID != nil {
		s := f.OriginExecutionID.String()
		v.OriginExecutionID = &s
	}
	return v
}

func (h *ProjectFileHandler) List(w http.ResponseWriter, r *http.Request) {
	project, _, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	files, err := h.files.List(r.Context(), project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	resp := struct {
		Files      []projectFileResponse `json:"files"`
		TotalBytes int64                 `json:"totalBytes"`
		MaxBytes   int64                 `json:"maxFileBytes"`
	}{Files: make([]projectFileResponse, 0, len(files)), MaxBytes: domain.MaxProjectFileBytes}
	for _, f := range files {
		resp.Files = append(resp.Files, projectFileResponseFrom(f))
		resp.TotalBytes += f.SizeBytes
	}
	writeJSON(w, http.StatusOK, resp)
}

// Upload reads the multipart body as a stream (never buffering the file) and seals it on the fly.
func (h *ProjectFileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, domain.MaxProjectFileBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "send the file as multipart/form-data in a field named \"file\""})
		return
	}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			writeError(w, &domain.ValidationError{Message: "no file field in the upload"})
			return
		}
		if err != nil {
			writeUploadError(w, err)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		file, err := h.files.Upload(r.Context(), project.ID, part.FileName(), part, identity.Subject)
		_ = part.Close()
		if err != nil {
			writeUploadError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, projectFileResponseFrom(file))
		return
	}
}

func writeUploadError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) || errors.Is(err, application.ErrFileTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Code: "file_too_large", Message: "o arquivo passa do limite de 100 MB"})
		return
	}
	writeError(w, err)
}

// Content streams the decrypted file. Only kinds that can't run script are served inline, and
// every response is sandboxed (CSP) and marked nosniff, so even a hostile upload can't execute
// on the API's origin.
func (h *ProjectFileHandler) Content(w http.ResponseWriter, r *http.Request) {
	project, _, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "fileId must be a valid UUID"})
		return
	}
	file, content, closer, err := h.files.Open(r.Context(), project.ID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	defer closer.Close()

	disposition := "attachment"
	contentType := "application/octet-stream"
	if file.Inline() && r.URL.Query().Get("download") != "1" {
		disposition = "inline"
		contentType = file.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(file.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, content); err != nil {
		// Headers are already out; an authentication failure mid-stream just cuts the body short
		// (the client sees a truncated download, never unauthenticated plaintext past the bad chunk).
		log.Printf("project files: streaming %s failed: %v", file.ID, err)
	}
}

func (h *ProjectFileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "fileId must be a valid UUID"})
		return
	}
	if err := h.files.Delete(r.Context(), project.ID, id, identity.Subject, identity.HasScope("projects:manage")); err != nil {
		if errors.Is(err, application.ErrForbidden) {
			writeAuthError(w, http.StatusForbidden, "only an admin or whoever uploaded the file can remove it")
			return
		}
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
