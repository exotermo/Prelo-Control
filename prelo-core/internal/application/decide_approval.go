package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/exotermo/prelo-core/internal/domain"
)

// DecideApprovalUseCase is the only path that can move a REQUIRE_APPROVAL tool call forward.
// Approving here is what actually runs the tool — a human's "yes" and the execution happen in
// the same call, so an approved-but-never-run request can't silently linger.
//
// When the approval belongs to a suspended agent-loop execution (Fase B — as opposed to a
// direct, manual /tools/.../invoke call, which never suspends anything), deciding it also
// completes that execution's pending ledger turn and resumes the job — the loop itself never
// polls for its own approval; whoever decides it is what wakes it back up. The one exception is
// delegate_to_agent (Fase C): approving it only creates the child Task — the parent execution
// must stay suspended (now waiting on the child, not the approval) until that child finishes.
type DecideApprovalUseCase struct {
	codes       ApprovalCodeRepository
	approvals   ApprovalRepository
	calls       ToolCallRepository
	tools       ToolRegistry
	executions  ExecutionRepository
	turns       ExecutionTurnRepository
	suspensions ExecutionSuspensionRepository
	jobs        ExecutionJobRepository
}

func NewDecideApprovalUseCase(approvals ApprovalRepository, calls ToolCallRepository, tools ToolRegistry, executions ExecutionRepository, turns ExecutionTurnRepository, suspensions ExecutionSuspensionRepository, jobs ExecutionJobRepository) *DecideApprovalUseCase {
	return &DecideApprovalUseCase{approvals: approvals, calls: calls, tools: tools, executions: executions, turns: turns, suspensions: suspensions, jobs: jobs}
}

// SetCodeRepository enables Fase T's WhatsApp decisions (by short code) and expiry.
func (uc *DecideApprovalUseCase) SetCodeRepository(codes ApprovalCodeRepository) { uc.codes = codes }

// DecideByCode is the owner's WhatsApp answer ("SIM K7Q2"). It goes through exactly the same
// Approve/Deny as the dashboard, so a request is still decided at most once; an expired or
// already-decided one comes back as an InvalidTransitionError the caller reports to the owner.
func (uc *DecideApprovalUseCase) DecideByCode(ctx context.Context, code string, approve bool, decidedBy string) (domain.ApprovalRequest, error) {
	if uc.codes == nil {
		return domain.ApprovalRequest{}, ErrApprovalNotFound
	}
	approval, err := uc.codes.FindLatestByShortCode(ctx, domain.NormalizeShortCode(code))
	if err != nil {
		return domain.ApprovalRequest{}, err
	}
	if approve {
		return uc.Approve(ctx, approval.ID, decidedBy)
	}
	return uc.Deny(ctx, approval.ID, decidedBy)
}

// ExpireDue closes pending requests whose deadline passed and wakes their executions up with the
// refusal, so nothing waits forever on an owner who never answered.
func (uc *DecideApprovalUseCase) ExpireDue(ctx context.Context) (int, error) {
	if uc.codes == nil {
		return 0, nil
	}
	due, err := uc.codes.ListDuePending(ctx, 20)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, approval := range due {
		closed, err := approval.Expire()
		if err != nil {
			continue
		}
		if _, err := uc.approvals.Update(ctx, closed); err != nil {
			continue // decided concurrently — whoever won handles the execution
		}
		if call, err := uc.calls.FindByID(ctx, approval.ToolCallID); err == nil {
			outcome := domain.OutcomeDenied
			_, _ = uc.calls.Update(ctx, call.Resolved(outcome, nil, nil))
		}
		if err := uc.resumeSuspendedExecution(ctx, approval.ID, "expirou: o dono não respondeu a tempo — a ação não foi executada"); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

func (uc *DecideApprovalUseCase) Approve(ctx context.Context, id domain.ApprovalRequestID, decidedBy string) (domain.ApprovalRequest, error) {
	approval, err := uc.approvals.FindByID(ctx, id)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}
	approved, err := approval.Approve(decidedBy)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}
	approved, err = uc.approvals.Update(ctx, approved)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}

	// The ApprovalRequest is already persisted as APPROVED above — that part genuinely
	// succeeded. A failure from here on must still propagate (not silently report success):
	// swallowing it here used to leave the execution stuck forever with no visible error (an
	// AWAITING_RESUME job is deliberately excluded from the sweeper's orphan recovery, so
	// nothing would ever retry it), and the caller would have no way to know something was
	// left undone.
	call, err := uc.calls.FindByID(ctx, approval.ToolCallID)
	if err != nil {
		return domain.ApprovalRequest{}, fmt.Errorf("approval %s recorded, but its tool call could not be loaded: %w", id, err)
	}
	execution, err := uc.executions.FindByID(ctx, call.ExecutionID)
	if err != nil {
		return domain.ApprovalRequest{}, fmt.Errorf("approval %s recorded, but its execution could not be loaded: %w", id, err)
	}

	var resolvedCall domain.ToolCall
	executor, ok := uc.tools.Find(call.ToolName)
	if !ok {
		message := "tool no longer registered"
		outcome := domain.OutcomeFailed
		resolvedCall, _ = uc.calls.Update(ctx, call.Resolved(outcome, nil, &message))
	} else {
		resolvedCall, _ = executeAndResolve(ctx, uc.calls, executor, execution, call)
	}

	// delegate_to_agent (Fase C): a successful Execute() here has already created and enqueued
	// the child Task — resolvedCall.Result is its TaskID. The parent stays suspended, just
	// against a new SUBTASK wait instead of the APPROVAL one that just got decided; the ledger
	// turn is deliberately left open until the child's real result comes back.
	if resolvedCall.ToolName == domain.DelegateToolName && resolvedCall.Outcome != nil && *resolvedCall.Outcome == domain.OutcomeExecuted && resolvedCall.Result != nil {
		if err := uc.suspendForSubtask(ctx, id, *resolvedCall.Result); err != nil {
			return domain.ApprovalRequest{}, err
		}
		return approved, nil
	}

	outcomeText := "approved, but produced no result"
	if resolvedCall.Outcome != nil {
		switch *resolvedCall.Outcome {
		case domain.OutcomeExecuted:
			if resolvedCall.Result != nil {
				outcomeText = *resolvedCall.Result
			}
		case domain.OutcomeFailed:
			if resolvedCall.Error != nil {
				outcomeText = "error: " + *resolvedCall.Error
			}
		}
	}
	if err := uc.resumeSuspendedExecution(ctx, id, outcomeText); err != nil {
		return domain.ApprovalRequest{}, err
	}
	return approved, nil
}

func (uc *DecideApprovalUseCase) Deny(ctx context.Context, id domain.ApprovalRequestID, decidedBy string) (domain.ApprovalRequest, error) {
	approval, err := uc.approvals.FindByID(ctx, id)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}
	denied, err := approval.Deny(decidedBy)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}
	denied, err = uc.approvals.Update(ctx, denied)
	if err != nil {
		return domain.ApprovalRequest{}, err
	}

	if call, err := uc.calls.FindByID(ctx, approval.ToolCallID); err == nil {
		outcome := domain.OutcomeDenied
		_, _ = uc.calls.Update(ctx, call.Resolved(outcome, nil, nil))
	}
	if err := uc.resumeSuspendedExecution(ctx, id, "negado pelo dono — a ação não foi executada"); err != nil {
		return domain.ApprovalRequest{}, err
	}
	return denied, nil
}

// resumeSuspendedExecution is a no-op (not an error) when this approval was never tied to a
// suspended agent-loop execution — a direct/manual tool invocation never creates an
// ExecutionSuspension in the first place, and deciding it should behave exactly as before
// Fase B: run or don't, nothing else to wake up.
func (uc *DecideApprovalUseCase) resumeSuspendedExecution(ctx context.Context, approvalID domain.ApprovalRequestID, outcomeText string) error {
	suspension, found, err := uc.suspensions.FindActiveByResumeKey(ctx, domain.SuspensionApproval, approvalID.String())
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	if err := uc.completeLatestOpenTurn(ctx, suspension.ExecutionID, outcomeText); err != nil {
		return err
	}
	// A concurrent/duplicate decision already resolved this suspension and is the one that
	// will (or already did) call jobs.Resume — back off instead of resuming a second time.
	if err := uc.suspensions.Resolve(ctx, suspension.ID); err != nil {
		if errors.Is(err, ErrExecutionSuspensionAlreadyResolved) {
			return nil
		}
		return err
	}
	return uc.jobs.Resume(ctx, suspension.ExecutionID)
}

// suspendForSubtask is resumeSuspendedExecution's Fase C counterpart: it resolves the APPROVAL
// suspension the same way, but instead of resuming the job it immediately re-suspends the same
// execution under SUBTASK — the parent has nothing left to do until the child Task finishes,
// which is a separate, later event this use case has no part in (ProcessJobUseCase's own hook
// resolves it — see resolveParentSubtaskSuspension).
func (uc *DecideApprovalUseCase) suspendForSubtask(ctx context.Context, approvalID domain.ApprovalRequestID, childTaskID string) error {
	suspension, found, err := uc.suspensions.FindActiveByResumeKey(ctx, domain.SuspensionApproval, approvalID.String())
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := uc.suspensions.Resolve(ctx, suspension.ID); err != nil {
		if errors.Is(err, ErrExecutionSuspensionAlreadyResolved) {
			return nil
		}
		return err
	}
	return uc.suspensions.Insert(ctx, domain.NewExecutionSuspension(suspension.ExecutionID, domain.SuspensionSubtask, childTaskID))
}

func (uc *DecideApprovalUseCase) completeLatestOpenTurn(ctx context.Context, executionID domain.ExecutionID, outcomeText string) error {
	turns, err := uc.turns.ListByExecution(ctx, executionID)
	if err != nil {
		return err
	}
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].CompletedAt == nil {
			_, err := uc.turns.Update(ctx, turns[i].Completed(outcomeText))
			return err
		}
	}
	return nil
}
