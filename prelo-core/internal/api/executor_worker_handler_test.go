package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type apiWorkerRepo struct {
	workers map[uuid.UUID]application.ExecutorWorker
}

func (r *apiWorkerRepo) Insert(_ context.Context, w application.ExecutorWorker) error {
	r.workers[w.ID] = w
	return nil
}
func (r *apiWorkerRepo) FindByTokenHash(_ context.Context, hash []byte) (application.ExecutorWorker, error) {
	for _, w := range r.workers {
		if w.Enabled && string(w.TokenHash) == string(hash) {
			return w, nil
		}
	}
	return application.ExecutorWorker{}, application.ErrExecutorWorkerNotFound
}
func (r *apiWorkerRepo) FindByID(_ context.Context, id uuid.UUID) (application.ExecutorWorker, error) {
	w, ok := r.workers[id]
	if !ok {
		return application.ExecutorWorker{}, application.ErrExecutorWorkerNotFound
	}
	return w, nil
}
func (r *apiWorkerRepo) List(context.Context) ([]application.ExecutorWorker, error) {
	out := []application.ExecutorWorker{}
	for _, w := range r.workers {
		out = append(out, w)
	}
	return out, nil
}
func (r *apiWorkerRepo) Touch(_ context.Context, id uuid.UUID) error {
	if !r.workers[id].Enabled {
		return application.ErrExecutorWorkerUnauthorized
	}
	return nil
}
func (r *apiWorkerRepo) Revoke(_ context.Context, id uuid.UUID) error {
	w, ok := r.workers[id]
	if !ok {
		return application.ErrExecutorWorkerNotFound
	}
	w.Enabled = false
	r.workers[id] = w
	return nil
}

func TestExecutorWorkerRoutes_OnlyHumanAdminRegistersAndHeartbeatCannotExecute(t *testing.T) {
	project, err := domain.NewProject("Teste", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	repo := &apiWorkerRepo{workers: map[uuid.UUID]application.ExecutorWorker{}}
	mux := http.NewServeMux()
	RegisterExecutorWorkerRoutes(mux, NewExecutorWorkerHandler(application.NewExecutorWorkerService(repo), fakeProjectFinder{projects: map[string]domain.Project{project.ID.String(): project}}))
	path := "/api/v1/executor-workers"
	admin := AuthContext{Subject: "admin", TokenUse: "dashboard", Scopes: map[string]struct{}{"settings:manage": {}}}
	create := func(identity AuthContext) *httptest.ResponseRecorder {
		req := withIdentity(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"vm-1","projectId":"`+project.ID.String()+`","imageDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)), identity)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	if w := create(AuthContext{Subject: "agent", TokenUse: "api_key", Scopes: admin.Scopes}); w.Code != http.StatusForbidden {
		t.Fatalf("machine registered worker: %d", w.Code)
	}
	if w := create(AuthContext{Subject: "operator", TokenUse: "dashboard"}); w.Code != http.StatusForbidden {
		t.Fatalf("operator registered worker: %d", w.Code)
	}
	w := create(admin)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	var registered struct {
		Worker executorWorkerView `json:"worker"`
		Token  string             `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if registered.Token == "" || registered.Worker.RuntimeStatus != "DISABLED" || registered.Worker.ProjectID != project.ID.String() {
		t.Fatal("bad registration response")
	}
	list := withIdentity(httptest.NewRequest(http.MethodGet, path, nil), admin)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, list)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), registered.Token) || strings.Contains(w.Body.String(), "tokenHash") {
		t.Fatal("list leaked secret")
	}
	hb := httptest.NewRequest(http.MethodPost, path+"/heartbeat", nil)
	hb.Header.Set("Authorization", "Bearer "+registered.Token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, hb)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"canExecute":false`) {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	revoke := withIdentity(httptest.NewRequest(http.MethodDelete, path+"/"+registered.Worker.ID, nil), admin)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, revoke)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, hb)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked worker heartbeat: %d", w.Code)
	}
}
