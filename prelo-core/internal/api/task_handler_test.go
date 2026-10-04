package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type fakeTaskRepo struct {
	mu    sync.Mutex
	tasks map[string]domain.Task
}

func newFakeTaskRepo() *fakeTaskRepo { return &fakeTaskRepo{tasks: map[string]domain.Task{}} }

func (f *fakeTaskRepo) Insert(_ context.Context, task domain.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID.String()] = task
	return nil
}

func (f *fakeTaskRepo) FindByID(_ context.Context, id domain.TaskID) (domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id.String()]
	if !ok {
		return domain.Task{}, application.ErrTaskNotFound
	}
	return t, nil
}

func (f *fakeTaskRepo) FindChildren(_ context.Context, parentID domain.TaskID) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var children []domain.Task
	for _, t := range f.tasks {
		if t.ParentTaskID != nil && *t.ParentTaskID == parentID {
			children = append(children, t)
		}
	}
	return children, nil
}

func (f *fakeTaskRepo) ListRoots(_ context.Context, limit int) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var roots []domain.Task
	for _, t := range f.tasks {
		if t.ParentTaskID == nil {
			roots = append(roots, t)
		}
	}
	if len(roots) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

func (f *fakeTaskRepo) ListActiveRoots(_ context.Context, limit int) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var roots []domain.Task
	for _, t := range f.tasks {
		if t.ParentTaskID == nil && t.Status != domain.TaskCompleted && t.Status != domain.TaskFailed {
			roots = append(roots, t)
		}
	}
	if len(roots) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

func matchesProject(t domain.Task, projectID *uuid.UUID) bool {
	if projectID == nil {
		return t.ProjectID == nil
	}
	return t.ProjectID != nil && t.ProjectID.Value == *projectID
}

func (f *fakeTaskRepo) ListRootsByProject(_ context.Context, projectID *uuid.UUID, limit int) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var roots []domain.Task
	for _, t := range f.tasks {
		if t.ParentTaskID == nil && matchesProject(t, projectID) {
			roots = append(roots, t)
		}
	}
	if len(roots) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

func (f *fakeTaskRepo) ListActiveRootsByProject(_ context.Context, projectID *uuid.UUID, limit int) ([]domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var roots []domain.Task
	for _, t := range f.tasks {
		if t.ParentTaskID == nil && t.Status != domain.TaskCompleted && t.Status != domain.TaskFailed && matchesProject(t, projectID) {
			roots = append(roots, t)
		}
	}
	if len(roots) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

func (f *fakeTaskRepo) Update(_ context.Context, task domain.Task) (domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID.String()] = task
	return task, nil
}

// fakeTenantTaskRepo additionally implements tenantTaskReader, so tests can exercise the
// tenant-scoped branch of TaskHandler (and, per tenantIdentity, confirm a dashboard session
// never takes it even when an identity is present in the request context).
type fakeTenantTaskRepo struct{ *fakeTaskRepo }

func newFakeTenantTaskRepo() *fakeTenantTaskRepo { return &fakeTenantTaskRepo{newFakeTaskRepo()} }

func (f *fakeTenantTaskRepo) FindByIDForTenant(ctx context.Context, id domain.TaskID, tenantID string) (domain.Task, error) {
	task, err := f.FindByID(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if task.TenantID != tenantID {
		return domain.Task{}, application.ErrTaskNotFound
	}
	return task, nil
}

func (f *fakeTenantTaskRepo) ListRootsForTenant(ctx context.Context, tenantID string, limit int) ([]domain.Task, error) {
	all, err := f.ListRoots(ctx, limit)
	if err != nil {
		return nil, err
	}
	var scoped []domain.Task
	for _, t := range all {
		if t.TenantID == tenantID {
			scoped = append(scoped, t)
		}
	}
	return scoped, nil
}

type fakeExecutionRepo struct {
	mu         sync.Mutex
	executions map[string]domain.Execution
}

func newFakeExecutionRepo() *fakeExecutionRepo {
	return &fakeExecutionRepo{executions: map[string]domain.Execution{}}
}

func (f *fakeExecutionRepo) Insert(_ context.Context, e domain.Execution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions[e.ID.String()] = e
	return nil
}

func (f *fakeExecutionRepo) FindByID(_ context.Context, id domain.ExecutionID) (domain.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.executions[id.String()]
	if !ok {
		return domain.Execution{}, application.ErrExecutionNotFound
	}
	return e, nil
}

func (f *fakeExecutionRepo) Update(_ context.Context, e domain.Execution) (domain.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions[e.ID.String()] = e
	return e, nil
}

func (f *fakeExecutionRepo) FindByTaskID(_ context.Context, taskID domain.TaskID) (domain.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.executions {
		if e.TaskID == taskID {
			return e, nil
		}
	}
	return domain.Execution{}, application.ErrExecutionNotFound
}

type fakeManualContextRepo struct{}

func (fakeManualContextRepo) Save(context.Context, domain.TaskID, []domain.ManualContextItem) error {
	return nil
}
func (fakeManualContextRepo) FindByTaskID(context.Context, domain.TaskID) ([]domain.ManualContextItem, error) {
	return nil, nil
}

type fakeAgentRegistry struct {
	known map[string]domain.AgentDefinition
}

func newFakeAgentRegistry() fakeAgentRegistry {
	general, _ := domain.NewAgentDefinition(domain.AgentID{Value: "general"}, domain.AgentTypeGeneral, "1", "be helpful", "general-chat", nil, "General", "")
	return fakeAgentRegistry{known: map[string]domain.AgentDefinition{"general": general}}
}
func (f fakeAgentRegistry) Find(id domain.AgentID) (domain.AgentDefinition, bool) {
	d, ok := f.known[id.Value]
	return d, ok
}
func (f fakeAgentRegistry) FindRequired(id domain.AgentID) (domain.AgentDefinition, error) {
	d, ok := f.Find(id)
	if !ok {
		return domain.AgentDefinition{}, &domain.ErrUnknownAgent{AgentID: id.Value}
	}
	return d, nil
}

func newTestTaskHandler() (*TaskHandler, *fakeTaskRepo, *fakeExecutionRepo) {
	taskRepo := newFakeTaskRepo()
	execRepo := newFakeExecutionRepo()
	createUseCase := application.NewCreateTaskUseCase(taskRepo, fakeManualContextRepo{}, newFakeAgentRegistry())
	return NewTaskHandler(createUseCase, taskRepo, execRepo, nil), taskRepo, execRepo
}

func TestTaskHandler_Create_Success(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"description":"do the thing","context":[{"name":"n","content":"c"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "CREATED" || resp.AgentID != "general" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestTaskHandler_Create_UnknownAgent(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"description":"do the thing","agentId":"ghost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Code != "unknown_agent" {
		t.Fatalf("expected code unknown_agent, got %s", errResp.Code)
	}
}

// Regression coverage for 2026-10-02: every inbound WhatsApp message was showing up in
// prelo-dashboard's Tasks list indistinguishable from a task someone actually designated —
// see domain.TaskSource.
func TestTaskHandler_Create_DefaultsSourceToManual(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"description":"do the thing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp taskResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Source != "MANUAL" {
		t.Fatalf("expected source MANUAL by default, got %q", resp.Source)
	}
}

func TestTaskHandler_Create_AcceptsMessagingSource(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"description":"responder no whatsapp","source":"MESSAGING"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp taskResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Source != "MESSAGING" {
		t.Fatalf("expected source MESSAGING, got %q", resp.Source)
	}
}

func TestTaskHandler_Create_RejectsUnknownSource(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	body := `{"description":"do the thing","source":"BOGUS"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskHandler_Create_ContextBudgetExceeded(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	big := strings.Repeat("a", domain.MaxManualContentLength)
	var buf bytes.Buffer
	buf.WriteString(`{"description":"d","context":[`)
	for i := 0; i < 4; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"name":"n` + string(rune('0'+i)) + `","content":"` + big + `"}`)
	}
	buf.WriteString(`]}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", &buf)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskHandler_Get_NotFound(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Code != "task_not_found" {
		t.Fatalf("expected code task_not_found, got %s", errResp.Code)
	}
}

func TestTaskHandler_Get_Success(t *testing.T) {
	handler, repo, _ := newTestTaskHandler()
	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = repo.Insert(context.Background(), task)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskHandler_GetExecution_Success(t *testing.T) {
	handler, taskRepo, execRepo := newTestTaskHandler()
	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(context.Background(), task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(context.Background(), exec)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/executions/"+exec.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp executionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "PENDING" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestTaskHandler_GetExecution_WrongTask404s(t *testing.T) {
	handler, taskRepo, execRepo := newTestTaskHandler()
	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	otherTask, _ := domain.NewTask("other", agentID)
	_ = taskRepo.Insert(context.Background(), task)
	_ = taskRepo.Insert(context.Background(), otherTask)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(context.Background(), exec)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+otherTask.ID.String()+"/executions/"+exec.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Code != "execution_not_found" {
		t.Fatalf("expected code execution_not_found, got %s", errResp.Code)
	}
}

func TestTaskHandler_GetExecution_NotFound(t *testing.T) {
	handler, _, _ := newTestTaskHandler()
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/00000000-0000-0000-0000-000000000000/executions/00000000-0000-0000-0000-000000000001", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestTaskHandler_LatestExecution_ResolvesWithoutKnowingTheExecutionID proves the Fase E
// convenience lookup — and that its literal "latest" segment doesn't get swallowed by the
// adjacent {executionId} wildcard route at the same path position.
func TestTaskHandler_LatestExecution_ResolvesWithoutKnowingTheExecutionID(t *testing.T) {
	handler, taskRepo, execRepo := newTestTaskHandler()
	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("desc", agentID)
	_ = taskRepo.Insert(context.Background(), task)
	exec := domain.NewPendingExecution(task.ID, agentID)
	_ = execRepo.Insert(context.Background(), exec)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+task.ID.String()+"/executions/latest", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp executionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ExecutionID != exec.ID.String() {
		t.Fatalf("expected the task's execution, got %+v", resp)
	}
}

// TestTaskHandler_List_OnlyReturnsRootTasks proves the Fase E list endpoint excludes delegated
// sub-tasks — they belong under their parent's tree (Fase D), not mixed into the top-level list.
func TestTaskHandler_List_OnlyReturnsRootTasks(t *testing.T) {
	handler, repo, _ := newTestTaskHandler()
	agentID, _ := domain.NewAgentID("general")
	root, _ := domain.NewTask("root task", agentID)
	_ = repo.Insert(context.Background(), root)
	child, _ := domain.NewSubtask("child task", agentID, root)
	_ = repo.Insert(context.Background(), child)

	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 || resp[0].ID != root.ID.String() {
		t.Fatalf("expected only the root task, got %+v", resp)
	}
}

// --- Fase G1 regression: a dashboard session must never be routed through tenant scoping ---

func withIdentity(req *http.Request, identity AuthContext) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), authContextKey{}, identity))
}

func TestTaskHandler_Create_TenantIdentity_IsScopedToThatTenant(t *testing.T) {
	repo := newFakeTenantTaskRepo()
	createUseCase := application.NewCreateTaskUseCase(repo, fakeManualContextRepo{}, newFakeAgentRegistry())
	handler := NewTaskHandler(createUseCase, repo, newFakeExecutionRepo(), nil)
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"description":"scoped"}`)),
		AuthContext{TokenUse: "integration", TenantID: "11111111-1111-1111-1111-111111111111"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.tasks) != 1 {
		t.Fatalf("expected exactly one task stored, got %d", len(repo.tasks))
	}
	for _, task := range repo.tasks {
		if task.TenantID != "11111111-1111-1111-1111-111111111111" {
			t.Fatalf("expected the task to carry the caller's tenant id, got %q", task.TenantID)
		}
	}
}

func TestTaskHandler_Create_DashboardSession_IsNeverTenantScoped(t *testing.T) {
	repo := newFakeTenantTaskRepo()
	createUseCase := application.NewCreateTaskUseCase(repo, fakeManualContextRepo{}, newFakeAgentRegistry())
	handler := NewTaskHandler(createUseCase, repo, newFakeExecutionRepo(), nil)
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	// A dashboard session carries no tenant at all — CreateForTenant would reject an empty
	// tenant id with a validation error; the handler must route it through the unscoped Create
	// instead (see tenantIdentity's doc comment).
	req := withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"description":"from the console"}`)),
		AuthContext{TokenUse: "dashboard", Subject: "some-user-id"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskHandler_List_DashboardSession_SeesTasksAcrossEveryTenant(t *testing.T) {
	repo := newFakeTenantTaskRepo()
	agentID, _ := domain.NewAgentID("general")
	taskA, _ := domain.NewTask("tenant A's task", agentID)
	taskA.TenantID = "aaaaaaaa-1111-1111-1111-111111111111"
	_ = repo.Insert(context.Background(), taskA)
	taskB, _ := domain.NewTask("tenant B's task", agentID)
	taskB.TenantID = "bbbbbbbb-2222-2222-2222-222222222222"
	_ = repo.Insert(context.Background(), taskB)

	handler := NewTaskHandler(nil, repo, newFakeExecutionRepo(), nil)
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler, NewChatHandler(nil), &ToolHandler{}, &ApprovalHandler{}, &ObservabilityHandler{})

	req := withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil), AuthContext{TokenUse: "dashboard", Subject: "some-user-id"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []taskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected a dashboard session to see both tenants' tasks, got %+v", resp)
	}
}
