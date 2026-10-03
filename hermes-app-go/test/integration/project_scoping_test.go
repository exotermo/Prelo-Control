package integration

import (
	"context"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/persistence/postgres"
)

// TestTaskRepository_ByProjectScoping exercises the Fase W filter end to end against real
// Postgres: a task in Project A, one in Project B, and one in the "unassigned" bucket must
// each only show up under their own filter.
func TestTaskRepository_ByProjectScoping(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewTaskRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	ctx := context.Background()
	agentID, _ := domain.NewAgentID("general")

	projectA := mustInsertProject(t, ctx, projects, "Project A")
	projectB := mustInsertProject(t, ctx, projects, "Project B")

	unassigned, _ := domain.NewTask("unassigned task", agentID)
	taskA, _ := domain.NewTask("project A task", agentID)
	taskA.ProjectID = &projectA
	taskB, _ := domain.NewTask("project B task", agentID)
	taskB.ProjectID = &projectB

	for _, task := range []domain.Task{unassigned, taskA, taskB} {
		if err := repo.Insert(ctx, task); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
	}

	roots, err := repo.ListRootsByProject(ctx, nil, 50)
	if err != nil {
		t.Fatalf("ListRootsByProject(nil) failed: %v", err)
	}
	assertOnlyTask(t, roots, unassigned.ID)

	roots, err = repo.ListRootsByProject(ctx, &projectA.Value, 50)
	if err != nil {
		t.Fatalf("ListRootsByProject(A) failed: %v", err)
	}
	assertOnlyTask(t, roots, taskA.ID)

	roots, err = repo.ListRootsByProject(ctx, &projectB.Value, 50)
	if err != nil {
		t.Fatalf("ListRootsByProject(B) failed: %v", err)
	}
	assertOnlyTask(t, roots, taskB.ID)
}

func assertOnlyTask(t *testing.T, tasks []domain.Task, expected domain.TaskID) {
	t.Helper()
	if len(tasks) != 1 || tasks[0].ID != expected {
		t.Fatalf("expected exactly task %s, got %+v", expected, tasks)
	}
}

func TestServerRepository_ByProjectScoping(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewServerRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	ctx := context.Background()

	projectA := mustInsertProject(t, ctx, projects, "Project A")

	unassigned, _ := domain.NewServer("unassigned-host", "10.0.0.1", 22, "root", "tester")
	unassigned = unassigned.WithCredential([]byte("enc"), "fp-1", time.Now().UTC())

	scoped, _ := domain.NewServer("project-a-host", "10.0.0.2", 22, "root", "tester")
	scoped = scoped.WithProject(&projectA).WithCredential([]byte("enc"), "fp-2", time.Now().UTC())

	for _, server := range []domain.Server{unassigned, scoped} {
		if err := repo.Insert(ctx, server); err != nil {
			t.Fatalf("insert failed: %v", err)
		}
	}

	servers, err := repo.ListByProject(ctx, nil)
	if err != nil {
		t.Fatalf("ListByProject(nil) failed: %v", err)
	}
	if len(servers) != 1 || servers[0].ID != unassigned.ID {
		t.Fatalf("expected only the unassigned server, got %+v", servers)
	}

	servers, err = repo.ListByProject(ctx, &projectA.Value)
	if err != nil {
		t.Fatalf("ListByProject(A) failed: %v", err)
	}
	if len(servers) != 1 || servers[0].ID != scoped.ID {
		t.Fatalf("expected only the project A server, got %+v", servers)
	}
}

func TestApprovalRepository_ListPendingByProjectScoping(t *testing.T) {
	pool := newTestPool(t)
	taskRepo := postgres.NewTaskRepository(pool)
	executionRepo := postgres.NewExecutionRepository(pool)
	toolCallRepo := postgres.NewToolCallRepository(pool)
	approvalRepo := postgres.NewApprovalRepository(pool)
	projects := postgres.NewProjectRepository(pool)
	ctx := context.Background()
	agentID, _ := domain.NewAgentID("general")

	projectA := mustInsertProject(t, ctx, projects, "Project A")

	// Unassigned task + its pending approval.
	unassignedTask, _ := domain.NewTask("unassigned", agentID)
	mustInsertTask(t, taskRepo, unassignedTask)
	unassignedApproval := mustCreatePendingApproval(t, ctx, executionRepo, toolCallRepo, approvalRepo, unassignedTask, agentID)

	// Project A task + its pending approval.
	scopedTask, _ := domain.NewTask("scoped", agentID)
	scopedTask.ProjectID = &projectA
	mustInsertTask(t, taskRepo, scopedTask)
	scopedApproval := mustCreatePendingApproval(t, ctx, executionRepo, toolCallRepo, approvalRepo, scopedTask, agentID)

	pending, err := approvalRepo.ListPendingByProject(ctx, nil)
	if err != nil {
		t.Fatalf("ListPendingByProject(nil) failed: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != unassignedApproval {
		t.Fatalf("expected only the unassigned approval, got %+v", pending)
	}

	pending, err = approvalRepo.ListPendingByProject(ctx, &projectA.Value)
	if err != nil {
		t.Fatalf("ListPendingByProject(A) failed: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != scopedApproval {
		t.Fatalf("expected only the project A approval, got %+v", pending)
	}
}

func mustInsertProject(t *testing.T, ctx context.Context, repo *postgres.ProjectRepository, name string) domain.ProjectID {
	t.Helper()
	project, err := domain.NewProject(name, nil, "tester")
	if err != nil {
		t.Fatalf("NewProject failed: %v", err)
	}
	if err := repo.Insert(ctx, project); err != nil {
		t.Fatalf("project insert failed: %v", err)
	}
	return project.ID
}

func mustInsertTask(t *testing.T, repo *postgres.TaskRepository, task domain.Task) {
	t.Helper()
	if err := repo.Insert(context.Background(), task); err != nil {
		t.Fatalf("task insert failed: %v", err)
	}
}

func mustCreatePendingApproval(t *testing.T, ctx context.Context, executions *postgres.ExecutionRepository, toolCalls *postgres.ToolCallRepository, approvals *postgres.ApprovalRepository, task domain.Task, agentID domain.AgentID) domain.ApprovalRequestID {
	t.Helper()
	execution := domain.Execution{ID: domain.NewExecutionID(), TaskID: task.ID, AgentID: agentID, Status: domain.ExecutionRunning}
	if err := executions.Insert(ctx, execution); err != nil {
		t.Fatalf("execution insert failed: %v", err)
	}
	call := domain.NewToolCall(task.ID, execution.ID, agentID, "echo", "{}", domain.RiskModerate, domain.DecisionRequireApproval)
	if err := toolCalls.Insert(ctx, call); err != nil {
		t.Fatalf("tool call insert failed: %v", err)
	}
	approval := domain.NewApprovalRequest(call.ID, "test scope", 15*time.Minute)
	if err := approvals.Insert(ctx, approval); err != nil {
		t.Fatalf("approval insert failed: %v", err)
	}
	return approval.ID
}
