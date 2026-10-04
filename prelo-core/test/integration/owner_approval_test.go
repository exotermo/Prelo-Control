package integration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type recordingNotifier struct {
	mu    sync.Mutex
	texts []string
}

func (n *recordingNotifier) NotifyOwners(_ context.Context, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.texts = append(n.texts, text)
	return nil
}

func suspendOnEcho(t *testing.T) (agentLoopFixture, *recordingNotifier, domain.ExecutionID, domain.ApprovalRequest) {
	t.Helper()
	fx := setupAgentLoopFixture(t, "echo")
	notifier := &recordingNotifier{}
	fx.loop.SetOwnerNotifier(application.NewOwnerApprovalNotifier(fx.approvalRepo, fx.toolCallRepo, fx.registry, notifier))
	fx.decide.SetCodeRepository(fx.approvalRepo)
	ctx := context.Background()

	agentID, _ := domain.NewAgentID("general")
	task, _ := domain.NewTask("repita isto", agentID)
	if err := fx.taskRepo.Insert(ctx, task); err != nil {
		t.Fatal(err)
	}
	enqueued, err := fx.enqueue.Enqueue(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.process.ProcessExecution(ctx, enqueued.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := fx.approvalRepo.ListPending(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending approval, got %d (%v)", len(pending), err)
	}
	return fx, notifier, enqueued.ID, pending[0]
}

// Fase T: the owner gets the request on WhatsApp (code, risk, impact) and answers by code.
func TestOwnerApproval_WhatsAppMessageAndDecisionByCode(t *testing.T) {
	fx, notifier, executionID, approval := suspendOnEcho(t)
	ctx := context.Background()

	if len(approval.ShortCode) != domain.ShortCodeLength {
		t.Fatalf("approval should carry a short code, got %q", approval.ShortCode)
	}
	if len(notifier.texts) != 1 {
		t.Fatalf("owner should be notified exactly once, got %d", len(notifier.texts))
	}
	msg := notifier.texts[0]
	for _, want := range []string{approval.ShortCode, "risco MODERADO", "*Impacto:*", "SIM " + approval.ShortCode, "NÃO " + approval.ShortCode, "echo"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message misses %q:\n%s", want, msg)
		}
	}

	decided, err := fx.decide.DecideByCode(ctx, strings.ToLower(approval.ShortCode), true, "whatsapp:+5541999990000")
	if err != nil || decided.Status != domain.ApprovalApproved || decided.DecidedBy == nil || *decided.DecidedBy != "whatsapp:+5541999990000" {
		t.Fatalf("decision by code failed: %+v %v", decided, err)
	}
	var invalid *domain.InvalidTransitionError
	if _, err := fx.decide.DecideByCode(ctx, approval.ShortCode, false, "whatsapp:+5541999990000"); !errors.As(err, &invalid) {
		t.Fatalf("a second answer must be refused, got %v", err)
	}
	if _, err := fx.decide.DecideByCode(ctx, "ZZZZ", true, "x"); !errors.Is(err, application.ErrApprovalNotFound) {
		t.Fatalf("unknown code should be not found, got %v", err)
	}

	execution, err := fx.process.ProcessExecution(ctx, executionID)
	if err != nil || execution.Status != domain.ExecutionCompleted {
		t.Fatalf("execution should resume and complete: %+v %v", execution, err)
	}
	// The resumed call carries the structured tool protocol: request then result, same call id.
	msgs := fx.llm.lastRequest.Messages
	request, result := msgs[len(msgs)-2], msgs[len(msgs)-1]
	if request.Role != "assistant" || request.ToolName != "echo" || result.Role != "tool" || result.ToolCallID == "" || result.ToolCallID != request.ToolCallID {
		t.Fatalf("unexpected tool turns in history: %+v / %+v", request, result)
	}
	if len(fx.llm.lastRequest.Tools) == 0 || len(fx.llm.lastRequest.Tools[0].InputSchema) == 0 {
		t.Fatal("tools must be offered with their argument schema")
	}
}

// Fase T: an approval nobody answers expires and the execution moves on instead of waiting forever.
func TestOwnerApproval_UnansweredRequestExpiresAndExecutionFinishes(t *testing.T) {
	fx, _, executionID, approval := suspendOnEcho(t)
	ctx := context.Background()
	if _, err := fx.pool.Exec(ctx, `UPDATE approval_requests SET expires_at = now() - interval '1 minute' WHERE id = $1`, approval.ID.Value); err != nil {
		t.Fatal(err)
	}
	n, err := fx.decide.ExpireDue(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 expired approval, got %d (%v)", n, err)
	}
	stored, _ := fx.approvalRepo.FindByID(ctx, approval.ID)
	if stored.Status != domain.ApprovalExpired {
		t.Fatalf("approval should be EXPIRED, got %s", stored.Status)
	}
	execution, err := fx.process.ProcessExecution(ctx, executionID)
	if err != nil || execution.Status != domain.ExecutionCompleted {
		t.Fatalf("execution should finish after expiry: %+v %v", execution, err)
	}
	msgs := fx.llm.lastRequest.Messages
	if last := msgs[len(msgs)-1]; last.Role != "tool" || !strings.Contains(last.Content, "expirou") {
		t.Fatalf("model should be told the action expired, got %+v", last)
	}
}
