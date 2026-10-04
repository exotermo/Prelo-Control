package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/filestore"
)

type memFiles struct {
	mu    sync.Mutex
	files map[uuid.UUID]domain.ProjectFile
}

func (m *memFiles) Insert(_ context.Context, f domain.ProjectFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[f.ID] = f
	return nil
}
func (m *memFiles) FindByID(_ context.Context, id uuid.UUID) (domain.ProjectFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[id]
	if !ok {
		return domain.ProjectFile{}, application.ErrProjectFileNotFound
	}
	return f, nil
}
func (m *memFiles) ListByProject(_ context.Context, p domain.ProjectID) ([]domain.ProjectFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ProjectFile
	for _, f := range m.files {
		if f.ProjectID == p && f.DeletedAt == nil {
			out = append(out, f)
		}
	}
	return out, nil
}
func (m *memFiles) SoftDelete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[id]
	now := f.CreatedAt
	f.DeletedAt = &now
	m.files[id] = f
	return nil
}

type fileFixture struct {
	handler  *ProjectFileHandler
	dir      string
	project  uuid.UUID
	member   uuid.UUID
	outsider uuid.UUID
}

func newFileFixture(t *testing.T) fileFixture {
	t.Helper()
	dir := t.TempDir()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	store, err := filestore.NewStore(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	project, member, outsider := uuid.New(), uuid.New(), uuid.New()
	service := application.NewProjectFileService(&memFiles{files: map[uuid.UUID]domain.ProjectFile{}}, filestore.Blobs{Store: store})
	projects := fakeProjectFinder{projects: map[string]domain.Project{project.String(): {ID: domain.ProjectID{Value: project}}}}
	members := fakeMembershipChecker{members: map[string]bool{project.String() + "/" + member.String(): true}}
	return fileFixture{handler: NewProjectFileHandler(service, projects, members), dir: dir, project: project, member: member, outsider: outsider}
}

func (f fileFixture) req(method, target string, body io.Reader, contentType string, user uuid.UUID, scopes ...string) *http.Request {
	r := httptest.NewRequest(method, target, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	set := map[string]struct{}{}
	for _, s := range scopes {
		set[s] = struct{}{}
	}
	r.SetPathValue("projectId", f.project.String())
	return r.WithContext(context.WithValue(r.Context(), authContextKey{}, AuthContext{Subject: user.String(), Scopes: set}))
}

func multipartBody(t *testing.T, name string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(content)
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func TestProjectFiles_UploadSealsReadsBackAndServesSafely(t *testing.T) {
	f := newFileFixture(t)
	pdf := append([]byte("%PDF-1.7\nconteudo secreto do contrato\n"), bytes.Repeat([]byte("x"), 3<<20)...)
	body, ct := multipartBody(t, "contrato.pdf", pdf)
	rec := httptest.NewRecorder()
	f.handler.Upload(rec, f.req(http.MethodPost, "/", body, ct, f.member))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var created projectFileResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	sum := sha256.Sum256(pdf)
	if created.Kind != "pdf" || !created.Inline || created.SHA256 != hex.EncodeToString(sum[:]) || created.SizeBytes != int64(len(pdf)) {
		t.Fatalf("unexpected metadata %+v", created)
	}

	blob, err := os.ReadFile(filepath.Join(f.dir, created.ID+".sealed"))
	if err != nil || bytes.Contains(blob, []byte("conteudo secreto")) {
		t.Fatalf("blob must exist and not contain plaintext (err=%v)", err)
	}

	req := f.req(http.MethodGet, "/", nil, "", f.member)
	req.SetPathValue("fileId", created.ID)
	rec = httptest.NewRecorder()
	f.handler.Content(rec, req)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), pdf) {
		t.Fatalf("content: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(rec.Header().Get("Content-Security-Policy"), "sandbox") || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("unsafe headers: %v", rec.Header())
	}
}

func TestProjectFiles_HTMLIsNeverServedInline(t *testing.T) {
	f := newFileFixture(t)
	body, ct := multipartBody(t, "pagina.pdf", []byte("<html><script>alert(1)</script></html>"))
	rec := httptest.NewRecorder()
	f.handler.Upload(rec, f.req(http.MethodPost, "/", body, ct, f.member))
	var created projectFileResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	req := f.req(http.MethodGet, "/", nil, "", f.member)
	req.SetPathValue("fileId", created.ID)
	rec = httptest.NewRecorder()
	f.handler.Content(rec, req)
	if created.Inline || rec.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("HTML disguised as PDF must download, got inline=%v headers=%v", created.Inline, rec.Header())
	}
}

func TestProjectFiles_OutsiderIsRefused(t *testing.T) {
	f := newFileFixture(t)
	rec := httptest.NewRecorder()
	f.handler.List(rec, f.req(http.MethodGet, "/", nil, "", f.outsider))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestProjectFiles_OnlyUploaderOrManagerDeletes(t *testing.T) {
	f := newFileFixture(t)
	other := uuid.New()
	f.handler.members = fakeMembershipChecker{members: map[string]bool{
		f.project.String() + "/" + f.member.String(): true, f.project.String() + "/" + other.String(): true}}
	body, ct := multipartBody(t, "n.txt", []byte("nota"))
	rec := httptest.NewRecorder()
	f.handler.Upload(rec, f.req(http.MethodPost, "/", body, ct, f.member))
	var created projectFileResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	del := func(user uuid.UUID, scopes ...string) int {
		req := f.req(http.MethodDelete, "/", nil, "", user, scopes...)
		req.SetPathValue("fileId", created.ID)
		rec := httptest.NewRecorder()
		f.handler.Delete(rec, req)
		return rec.Code
	}
	if code := del(other); code != http.StatusForbidden {
		t.Fatalf("another member must not delete: %d", code)
	}
	if code := del(f.member); code != http.StatusNoContent {
		t.Fatalf("uploader delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(f.dir, created.ID+".sealed")); !os.IsNotExist(err) {
		t.Fatal("deleted file's blob must be gone from disk")
	}
}

func TestRequiredScope_FilesAndSettings(t *testing.T) {
	cases := map[string]string{
		"POST /api/v1/projects/1/files":          "projects:read",
		"DELETE /api/v1/projects/1/files/2":      "projects:read",
		"GET /api/v1/projects/1/files/2/content": "projects:read",
		"PATCH /api/v1/projects/1":               "projects:manage",
		"GET /api/v1/agents":                     "tasks:read",
	}
	for route, want := range cases {
		parts := strings.SplitN(route, " ", 2)
		if got := requiredScope(parts[0], parts[1]); got != want {
			t.Errorf("%s: got %q want %q", route, got, want)
		}
	}
}
