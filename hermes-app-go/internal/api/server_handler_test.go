package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type fakeServerRepo struct {
	mu      sync.Mutex
	servers map[string]domain.Server
}

func newFakeServerRepo() *fakeServerRepo { return &fakeServerRepo{servers: map[string]domain.Server{}} }

func (f *fakeServerRepo) Insert(_ context.Context, s domain.Server) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.ID.String()] = s
	return nil
}
func (f *fakeServerRepo) FindByID(_ context.Context, id domain.ServerID) (domain.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[id.String()]
	if !ok {
		return domain.Server{}, application.ErrServerNotFound
	}
	return s, nil
}
func (f *fakeServerRepo) List(_ context.Context) ([]domain.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Server
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeServerRepo) ListByProject(_ context.Context, projectID *uuid.UUID) ([]domain.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Server
	for _, s := range f.servers {
		if projectID == nil {
			if s.ProjectID == nil {
				out = append(out, s)
			}
		} else if s.ProjectID != nil && s.ProjectID.Value == *projectID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeServerRepo) Update(_ context.Context, s domain.Server) (domain.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.ID.String()] = s
	return s, nil
}
func (f *fakeServerRepo) SoftDelete(_ context.Context, id domain.ServerID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.servers[id.String()]; !ok {
		return application.ErrServerNotFound
	}
	delete(f.servers, id.String())
	return nil
}

type fakeDialer struct {
	fingerprint string
	err         error
}

func (f fakeDialer) Register(context.Context, string, int, string, []byte) (string, error) {
	return f.fingerprint, f.err
}

type fakeCredentialCipher struct{}

func (fakeCredentialCipher) Encrypt(plaintext []byte, _ []byte) ([]byte, error) {
	return plaintext, nil
}

type fakeHealthChecker struct {
	snapshot application.ServerHealthSnapshot
	err      error
}

func (f fakeHealthChecker) Check(context.Context, domain.Server) (application.ServerHealthSnapshot, error) {
	return f.snapshot, f.err
}

func newTestServerHandler(dialer application.ServerRegistrationDialer, checker application.ServerHealthChecker) (*ServerHandler, *fakeServerRepo) {
	repo := newFakeServerRepo()
	register := application.NewRegisterServerUseCase(repo, dialer, fakeCredentialCipher{})
	checkHealth := application.NewCheckServerHealthUseCase(repo, checker)
	return NewServerHandler(register, checkHealth, repo), repo
}

func TestServerHandler_Create_RegistersOnlyAfterTheDialSucceeds(t *testing.T) {
	handler, repo := newTestServerHandler(fakeDialer{fingerprint: "SHA256:abc"}, fakeHealthChecker{})
	mux := http.NewServeMux()
	RegisterServerRoutes(mux, handler)

	body := `{"name":"prod-1","host":"10.0.0.1","sshPort":22,"sshUser":"root","privateKeyPem":"fake-pem"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp serverResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.HostKeyFingerprint != "SHA256:abc" {
		t.Fatalf("expected captured fingerprint in response, got %+v", resp)
	}
	if strings.Contains(rec.Body.String(), "fake-pem") {
		t.Fatal("response must never include the private key")
	}
	servers, _ := repo.List(context.Background())
	if len(servers) != 1 {
		t.Fatalf("expected the server to be persisted, got %d rows", len(servers))
	}
}

func TestServerHandler_Create_RejectsACredentialThatDoesNotAuthenticate(t *testing.T) {
	handler, repo := newTestServerHandler(fakeDialer{err: context.DeadlineExceeded}, fakeHealthChecker{})
	mux := http.NewServeMux()
	RegisterServerRoutes(mux, handler)

	body := `{"name":"prod-1","host":"10.0.0.1","sshPort":22,"sshUser":"root","privateKeyPem":"fake-pem"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if servers, _ := repo.List(context.Background()); len(servers) != 0 {
		t.Fatalf("a server must never be persisted when the registration dial fails, got %d rows", len(servers))
	}
}

func TestServerHandler_CheckHealth_PersistsTheOutcome(t *testing.T) {
	handler, repo := newTestServerHandler(fakeDialer{fingerprint: "SHA256:abc"}, fakeHealthChecker{
		snapshot: application.ServerHealthSnapshot{Status: domain.ServerStatusOnline, Uptime: "up 1 day"},
	})
	mux := http.NewServeMux()
	RegisterServerRoutes(mux, handler)

	created, err := handler.register.Register(context.Background(), "prod-1", "10.0.0.1", 22, "root", []byte("fake-pem"), "tester", nil)
	if err != nil {
		t.Fatalf("seed register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+created.ID.String()+"/health-check", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ := repo.FindByID(context.Background(), created.ID)
	if stored.LastStatus != domain.ServerStatusOnline || stored.LastCheckedAt == nil {
		t.Fatalf("expected the health result to be persisted, got %+v", stored)
	}
}

func TestServerHandler_Delete_RemovesTheServer(t *testing.T) {
	handler, repo := newTestServerHandler(fakeDialer{fingerprint: "SHA256:abc"}, fakeHealthChecker{})
	mux := http.NewServeMux()
	RegisterServerRoutes(mux, handler)

	created, _ := handler.register.Register(context.Background(), "prod-1", "10.0.0.1", 22, "root", []byte("fake-pem"), "tester", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/"+created.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := repo.FindByID(context.Background(), created.ID); err != application.ErrServerNotFound {
		t.Fatalf("expected the server to be gone, got err=%v", err)
	}
}
