package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

type recordingPublisher struct {
	mu     sync.Mutex
	events []map[string]any
	names  []string
}

func (p *recordingPublisher) Publish(_ context.Context, _ *domain.ProjectID, event string, data map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.names = append(p.names, event)
	p.events = append(p.events, data)
}

func deployPayload(sha, env string) (json.RawMessage, string) {
	raw := json.RawMessage(`{"repository":"exotermo/loja","commitSha":"` + sha + `","environment":"` + env + `","target":"loja.example.com","app":"loja","deployRequestId":"d-` + sha[:6] + `"}`)
	hash, _ := domain.PayloadHash(raw)
	return raw, hash
}

// PR-3 end to end at the service level, against the real migrations.
func TestActionRequestsFlow(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	projects := postgres.NewProjectRepository(pool)
	approvals := postgres.NewApprovalRepository(pool)
	actions := postgres.NewActionRequestRepository(pool)
	service := application.NewActionRequestService(actions, postgres.NewWorkspaceRepository(pool))
	events := &recordingPublisher{}
	service.SetNotifications(nil, events)
	decide := application.NewDecideApprovalUseCase(approvals, postgres.NewToolCallRepository(pool), nil,
		postgres.NewExecutionRepository(pool), postgres.NewExecutionTurnRepository(pool),
		postgres.NewExecutionSuspensionRepository(pool), postgres.NewExecutionJobRepository(pool))
	decide.SetCodeRepository(approvals)
	decide.SetActionListener(service)

	project, _ := domain.NewProject("Loja", nil, "tester")
	other, _ := domain.NewProject("Outro", nil, "tester")
	for _, p := range []domain.Project{project, other} {
		if err := projects.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	shaA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	payloadA, hashA := deployPayload(shaA, "production")
	in := application.CreateActionInput{Kind: "deploy", ProjectID: project.ID, Payload: payloadA, PayloadHash: hashA,
		Impact: "Publica a loja em produção", RequestedBy: "github:exotermo/loja@deploy.yml", IdempotencyKey: "loja:" + shaA + ":production"}

	if _, _, err := service.Create(ctx, other.ID, nil, in); !errors.Is(err, application.ErrNotProjectMember) {
		t.Fatalf("a key of another project must be refused, got %v", err)
	}
	view, created, err := service.Create(ctx, project.ID, nil, in)
	if err != nil || !created || view.Approval.Status != domain.ApprovalPending || len(view.Approval.ShortCode) != 4 || view.Action.Risk != domain.RiskHigh {
		t.Fatalf("create: %+v %v %v", view, created, err)
	}
	again, created, err := service.Create(ctx, project.ID, nil, in)
	if err != nil || created || again.Action.ID != view.Action.ID {
		t.Fatalf("same key + same payload must return the same request: %v %v", created, err)
	}
	payloadB, hashB := deployPayload("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "production")
	conflict := in
	conflict.Payload, conflict.PayloadHash = payloadB, hashB
	if _, _, err := service.Create(ctx, project.ID, nil, conflict); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("same key + another payload must conflict, got %v", err)
	}
	pending, _ := approvals.ListPendingByProject(ctx, &project.ID.Value)
	if len(pending) != 1 || !pending[0].IsAction() {
		t.Fatalf("the action approval must be listed for its project: %+v", pending)
	}
	if _, err := service.ReportResult(ctx, project.ID, view.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1}); !errors.Is(err, application.ErrActionNotApproved) {
		t.Fatalf("no result before approval, got %v", err)
	}

	if _, err := decide.DecideByCode(ctx, view.Approval.ShortCode, true, "whatsapp:+5541999990000"); err != nil {
		t.Fatal(err)
	}
	last := len(events.names) - 1
	if events.names[0] != domain.EventApprovalPending || events.names[last] != domain.EventActionDecided || events.events[last]["status"] != "APPROVED" || events.events[last]["payloadHash"] != hashA {
		t.Fatalf("action.decided must be published with status and hash: %v %v", events.names, events.events)
	}
	got, _ := service.Get(ctx, project.ID, view.Action.ID)
	if got.Approval.Status != domain.ApprovalApproved {
		t.Fatalf("GET is the source of truth: %s", got.Approval.Status)
	}
	if _, err := service.Get(ctx, other.ID, view.Action.ID); !errors.Is(err, application.ErrActionRequestNotFound) {
		t.Fatal("another project's key must not see the request")
	}

	if _, err := service.ReportResult(ctx, project.ID, view.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportResult(ctx, project.ID, view.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1}); err != nil {
		t.Fatal("a retried report is a no-op")
	}
	done, err := service.ReportResult(ctx, project.ID, view.Action.ID, application.ActionResultInput{Status: "SUCCEEDED", Sequence: 2, URL: "https://loja.example.com", Digest: "sha256:abc"})
	if err != nil || done.Action.Result.Status != "SUCCEEDED" || *done.Action.Result.URL != "https://loja.example.com" {
		t.Fatalf("result: %+v %v", done.Action.Result, err)
	}
	stale, _ := service.ReportResult(ctx, project.ID, view.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1})
	if stale.Action.Result.Status != "SUCCEEDED" {
		t.Fatal("an older sequence must never move the state back")
	}
	var history int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM action_request_results WHERE action_request_id = $1`, view.Action.ID).Scan(&history)
	if history != 2 {
		t.Fatalf("audit trail should keep 2 results, got %d", history)
	}

	// Denied: nothing may run.
	inB := in
	inB.Payload, inB.PayloadHash, inB.IdempotencyKey = payloadB, hashB, "loja:b:production"
	viewB, _, _ := service.Create(ctx, project.ID, nil, inB)
	if _, err := decide.DecideByCode(ctx, viewB.Approval.ShortCode, false, "whatsapp:+5541999990000"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportResult(ctx, project.ID, viewB.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1}); !errors.Is(err, application.ErrActionNotApproved) {
		t.Fatalf("a denied request accepts no result, got %v", err)
	}

	// Expired: the sweeper closes it and tells the executor.
	payloadC, hashC := deployPayload("cccccccccccccccccccccccccccccccccccccccc", "staging")
	inC := in
	inC.Payload, inC.PayloadHash, inC.IdempotencyKey = payloadC, hashC, "loja:c:staging"
	viewC, _, _ := service.Create(ctx, project.ID, nil, inC)
	_, _ = pool.Exec(ctx, `UPDATE approval_requests SET expires_at = now() - interval '1 minute' WHERE id = $1`, viewC.Approval.ID.Value)
	if n, err := decide.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("expire: %d %v", n, err)
	}
	if last := events.events[len(events.events)-1]; last["status"] != "EXPIRED" || last["actionRequestId"] != viewC.Action.ID.String() {
		t.Fatalf("expiry must be published: %v", last)
	}

	// Approved long ago and never started: too late to begin.
	payloadD, hashD := deployPayload("dddddddddddddddddddddddddddddddddddddddd", "production")
	inD := in
	inD.Payload, inD.PayloadHash, inD.IdempotencyKey = payloadD, hashD, "loja:d:production"
	viewD, _, _ := service.Create(ctx, project.ID, nil, inD)
	_, _ = decide.DecideByCode(ctx, viewD.Approval.ShortCode, true, "x")
	_, _ = pool.Exec(ctx, `UPDATE approval_requests SET expires_at = now() - interval '20 minutes' WHERE id = $1`, viewD.Approval.ID.Value)
	if _, err := service.ReportResult(ctx, project.ID, viewD.Action.ID, application.ActionResultInput{Status: "RUNNING", Sequence: 1, ReportedAt: time.Now()}); !errors.Is(err, application.ErrActionWindowClosed) {
		t.Fatalf("an approval unused for too long must not start, got %v", err)
	}
	list, _ := service.ListForProject(ctx, project.ID, "deploy", 10)
	if len(list) != 4 {
		t.Fatalf("project listing should show 4 requests, got %d", len(list))
	}
}
