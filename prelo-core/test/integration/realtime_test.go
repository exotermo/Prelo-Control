package integration

import (
	"context"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
	"github.com/exotermo/prelo-core/internal/infrastructure/realtime"
)

// PR-4: database triggers announce task and approval changes, and the hub delivers them.
func TestRealtimeNotificationsReachTheHub(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := realtime.NewHub()
	events, unsubscribe := hub.Subscribe(nil)
	defer unsubscribe()
	go hub.Listen(ctx, pool)

	next := func(kind string) realtime.Event {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case e := <-events:
				if e.Kind == kind {
					return e
				}
			case <-deadline:
				t.Fatalf("no %s event", kind)
			}
		}
	}
	next("resync") // sent when the listener (re)connects

	projects := postgres.NewProjectRepository(pool)
	tasks := postgres.NewTaskRepository(pool)
	project, _ := domain.NewProject("RT", nil, "tester")
	_ = projects.Insert(ctx, project)
	agent, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("tempo real", agent)
	task.ProjectID = &project.ID
	if err := tasks.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	e := next("task")
	if e.ID != task.ID.String() || e.ProjectID == nil || *e.ProjectID != project.ID.Value || e.Status != string(domain.TaskCreated) {
		t.Fatalf("task event: %+v", e)
	}
	if _, err := pool.Exec(ctx, `UPDATE tasks SET status = 'QUEUED' WHERE id = $1`, task.ID.Value); err != nil {
		t.Fatal(err)
	}
	if e := next("task"); e.Status != "QUEUED" {
		t.Fatalf("status change not announced: %+v", e)
	}
	if _, err := pool.Exec(ctx, `UPDATE tasks SET description = 'x' WHERE id = $1`, task.ID.Value); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-events:
		t.Fatalf("a change that is not the status must stay quiet, got %+v", e)
	case <-time.After(500 * time.Millisecond):
	}
}
