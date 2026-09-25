package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/gateway"
)

// maxLLMCalls bounds how many times one execution's loop may call the Gateway before it gives
// up and fails — the safety trap against a model (or a buggy mock) that never stops asking for
// tools. A hard ceiling, not yet tunable per agent/tenant.
const maxLLMCalls = 8

// toolResultPrefix must match MockProvider's TOOL_RESULT_PREFIX exactly (see its doc comment)
// — this is the mock-only convention for carrying a tool's result back as a plain message,
// until a real provider's structured tool_use/tool_result protocol is wired in.
const toolResultPrefix = "[tool_result:"

type LoopOutcome int

const (
	LoopCompleted LoopOutcome = iota
	LoopSuspended
	LoopFailed
)

// LoopResult is what ProcessJobUseCase acts on: LoopCompleted carries the final answer to
// finalize the Execution with (same shape as the single-call path always produced);
// LoopSuspended means the job is already AWAITING_RESUME — the caller must not touch job/Task
// state at all; LoopFailed carries the terminal error the caller should fail the Execution with.
type LoopResult struct {
	Outcome  LoopOutcome
	Content  string
	Request  string
	Model    string
	Provider string
	Err      error
}

// RunAgentLoopUseCase is the multi-turn agent loop (Fase B): calls the Gateway offering the
// agent's capable tools; a FINAL response ends the loop; a TOOL_USE response goes through the
// exact same permission gate a manual invocation would (InvokeToolUseCase) and either
// continues (ALLOW/DENY) or suspends the whole execution to wait on a human
// (REQUIRE_APPROVAL). ProcessJobUseCase owns claiming/leasing the job and finalizing
// Task/Execution state; this use case only owns what happens turn by turn.
type RunAgentLoopUseCase struct {
	llmGateway  LanguageModelGateway
	tools       ToolRegistry
	invokeTool  *InvokeToolUseCase
	turns       ExecutionTurnRepository
	suspensions ExecutionSuspensionRepository
	jobs        ExecutionJobRepository
}

func NewRunAgentLoopUseCase(llmGateway LanguageModelGateway, tools ToolRegistry, invokeTool *InvokeToolUseCase, turns ExecutionTurnRepository, suspensions ExecutionSuspensionRepository, jobs ExecutionJobRepository) *RunAgentLoopUseCase {
	return &RunAgentLoopUseCase{llmGateway: llmGateway, tools: tools, invokeTool: invokeTool, turns: turns, suspensions: suspensions, jobs: jobs}
}

// Run is safe to call again for the same Execution after a resume: it always rebuilds its
// message history from execution_turns first, so it picks up exactly where the last completed
// turn left off — it never re-asks the Gateway for a turn that already has a recorded Output,
// which is the actual idempotency guarantee (domain.TurnRequestID is what makes a duplicate
// attempt collide at the database level if this guarantee is ever violated by a bug).
func (uc *RunAgentLoopUseCase) Run(ctx context.Context, task domain.Task, agent domain.AgentDefinition, snapshot domain.ContextSnapshot, execution domain.Execution) (LoopResult, error) {
	existingTurns, err := uc.turns.ListByExecution(ctx, execution.ID)
	if err != nil {
		return LoopResult{}, err
	}

	messages := initialMessages(task, agent, snapshot)
	nextTurnNumber := 0
	llmCalls := 0
	for _, turn := range existingTurns {
		// A turn with neither Output nor Error is not "in progress" — a legitimate
		// REQUIRE_APPROVAL/SUBTASK wait always gets its turn completed (by
		// DecideApprovalUseCase or ProcessJobUseCase's completion hook) *before* the job is
		// ever resumed, so Run() is never re-entered while that's still open. Finding one here
		// means the worker that owned this execution crashed between the external call
		// actually happening and its outcome being persisted — we cannot tell whether the
		// Gateway/tool call went through, so silently dropping the turn and starting a fresh
		// one (as this used to do) risks asking the same question twice. Fail loudly instead;
		// this needs an operator to look at it, not a guess.
		if turn.Output == nil && turn.Error == nil {
			err := fmt.Errorf("execution has an unresolved turn %d (worker crashed before its outcome was persisted); refusing to guess and duplicate the external call", turn.TurnNumber)
			log.Printf("agent-loop: execution=%s turn=%d orphaned (no output/error recorded) — refusing to resume: %v", execution.ID, turn.TurnNumber, err)
			return LoopResult{Outcome: LoopFailed, Err: err}, nil
		}
		nextTurnNumber = turn.TurnNumber + 1
		switch turn.Kind {
		case domain.TurnLLMCall:
			llmCalls++
			if turn.Output != nil {
				messages = append(messages, gateway.Message{Role: "assistant", Content: *turn.Output})
			}
		case domain.TurnToolCall:
			if turn.Output != nil {
				messages = append(messages, gateway.Message{Role: "user", Content: toolResultPrefix + turn.Input + "] " + *turn.Output})
			}
		}
	}

	toolSpecs := toolSpecsFor(agent, uc.tools)

	for llmCalls < maxLLMCalls {
		turnNumber := nextTurnNumber
		requestID := domain.TurnRequestID(execution.ID, turnNumber)

		if already, found, err := uc.turns.FindByRequestID(ctx, requestID); err != nil {
			return LoopResult{}, err
		} else if found {
			// A turn for this number already exists but wasn't in existingTurns' tail logic
			// above (shouldn't happen in practice — existingTurns already covers this — but
			// guards against ever silently re-calling the Gateway for a turn that has a
			// recorded outcome).
			if already.Output != nil {
				messages = append(messages, gateway.Message{Role: "assistant", Content: *already.Output})
				nextTurnNumber = turnNumber + 1
				continue
			}
		}

		inputJSON, err := json.Marshal(messages)
		if err != nil {
			return LoopResult{}, err
		}
		turn := domain.NewExecutionTurn(execution.ID, turnNumber, domain.TurnLLMCall, string(inputJSON))
		if _, err := uc.turns.Insert(ctx, turn); err != nil {
			return LoopResult{}, err
		}

		chatReq := gateway.ChatRequest{
			ModelProfile: agent.ModelProfile,
			Messages:     messages,
			Tools:        toolSpecs,
			Metadata:     map[string]string{"taskId": task.ID.String(), "agentId": agent.AgentID.String()},
		}
		resp, err := uc.llmGateway.Chat(ctx, chatReq, requestID)
		if err != nil {
			// Logged with full detail (server-side only); persisted with the sanitized message
			// only — this turn's Error is served back unauthenticated via
			// GET /tasks/{id}/executions/{id}/turns.
			log.Printf("agent-loop: execution=%s turn=%d Gateway call failed: %v", execution.ID, turnNumber, err)
			if _, updateErr := uc.turns.Update(ctx, turn.Failed(sanitizedFailureMessage(err))); updateErr != nil {
				return LoopResult{}, updateErr
			}
			return LoopResult{Outcome: LoopFailed, Err: err}, nil
		}
		llmCalls++
		nextTurnNumber = turnNumber + 1

		if !resp.IsToolUse() {
			log.Printf("agent-loop: execution=%s turn=%d FINAL", execution.ID, turnNumber)
			if _, err := uc.turns.Update(ctx, turn.Completed(resp.Content)); err != nil {
				return LoopResult{}, err
			}
			return LoopResult{Outcome: LoopCompleted, Content: resp.Content, Request: resp.RequestID, Model: resp.Model, Provider: resp.Provider}, nil
		}
		log.Printf("agent-loop: execution=%s turn=%d requests tool=%s", execution.ID, turnNumber, resp.ToolName)

		if _, err := uc.turns.Update(ctx, turn.Completed(fmt.Sprintf("tool_use:%s(%s)", resp.ToolName, resp.ToolArgsJSON))); err != nil {
			return LoopResult{}, err
		}
		messages = append(messages, gateway.Message{Role: "assistant", Content: fmt.Sprintf("requesting tool %s with args %s", resp.ToolName, resp.ToolArgsJSON)})

		toolTurnNumber := nextTurnNumber
		toolTurnInput := resp.ToolName + "(" + resp.ToolArgsJSON + ")"
		toolTurn := domain.NewExecutionTurn(execution.ID, toolTurnNumber, domain.TurnToolCall, toolTurnInput)
		if _, err := uc.turns.Insert(ctx, toolTurn); err != nil {
			return LoopResult{}, err
		}
		nextTurnNumber = toolTurnNumber + 1

		toolCall, approvalID, err := uc.invokeTool.Invoke(ctx, execution.ID, resp.ToolName, resp.ToolArgsJSON)
		if err != nil {
			if _, updateErr := uc.turns.Update(ctx, toolTurn.Failed(err.Error())); updateErr != nil {
				return LoopResult{}, updateErr
			}
			return LoopResult{Outcome: LoopFailed, Err: err}, nil
		}

		log.Printf("agent-loop: execution=%s turn=%d tool=%s decision=%s", execution.ID, toolTurnNumber, resp.ToolName, toolCall.Decision)

		switch toolCall.Decision {
		case domain.DecisionRequireApproval:
			// Deliberately leave toolTurn unresolved (no Output/CompletedAt) — it is completed
			// later by DecideApprovalUseCase, once a human actually decides. The loop itself
			// stops here without calling the Gateway again.
			suspension := domain.NewExecutionSuspension(execution.ID, domain.SuspensionApproval, approvalID.String())
			if err := uc.suspensions.Insert(ctx, suspension); err != nil {
				return LoopResult{}, err
			}
			if err := uc.jobs.Suspend(ctx, execution.ID); err != nil {
				return LoopResult{}, err
			}
			log.Printf("agent-loop: execution=%s turn=%d suspended reason=APPROVAL approvalId=%s", execution.ID, toolTurnNumber, approvalID)
			return LoopResult{Outcome: LoopSuspended}, nil

		case domain.DecisionDeny:
			if _, err := uc.turns.Update(ctx, toolTurn.Completed("DENIED by permission policy")); err != nil {
				return LoopResult{}, err
			}
			messages = append(messages, gateway.Message{Role: "user", Content: toolResultPrefix + toolTurnInput + "] denied by permission policy"})

		case domain.DecisionAllow:
			// delegate_to_agent (Fase C), reached only if a future PermissionPolicy ever lets
			// it run without approval (DefaultPermissionPolicy never does — MODERATE always
			// requires one, so this path is normally exercised via DecideApprovalUseCase
			// instead). Symmetric handling either way: a successful Execute() has already
			// created+enqueued the child Task (toolCall.Result is its id) — suspend for
			// SUBTASK instead of treating this like a normal completed tool call.
			if resp.ToolName == domain.DelegateToolName && toolCall.Outcome != nil && *toolCall.Outcome == domain.OutcomeExecuted && toolCall.Result != nil {
				suspension := domain.NewExecutionSuspension(execution.ID, domain.SuspensionSubtask, *toolCall.Result)
				if err := uc.suspensions.Insert(ctx, suspension); err != nil {
					return LoopResult{}, err
				}
				if err := uc.jobs.Suspend(ctx, execution.ID); err != nil {
					return LoopResult{}, err
				}
				log.Printf("agent-loop: execution=%s turn=%d suspended reason=SUBTASK childTaskId=%s", execution.ID, toolTurnNumber, *toolCall.Result)
				return LoopResult{Outcome: LoopSuspended}, nil
			}

			outcome := ""
			if toolCall.Outcome != nil && *toolCall.Outcome == domain.OutcomeExecuted && toolCall.Result != nil {
				outcome = *toolCall.Result
			} else if toolCall.Error != nil {
				outcome = "error: " + *toolCall.Error
			}
			if _, err := uc.turns.Update(ctx, toolTurn.Completed(outcome)); err != nil {
				return LoopResult{}, err
			}
			messages = append(messages, gateway.Message{Role: "user", Content: toolResultPrefix + toolTurnInput + "] " + outcome})
		}
	}

	log.Printf("agent-loop: execution=%s exceeded %d LLM calls without a final answer", execution.ID, maxLLMCalls)
	return LoopResult{Outcome: LoopFailed, Err: fmt.Errorf("agent loop exceeded %d LLM calls without a final answer", maxLLMCalls)}, nil
}

// initialMessages mirrors process_job.go's former compilePrompt (etapa 6.5/single-call path) —
// same system+user shape, now just the seed for a loop's message history instead of the whole
// request.
func initialMessages(task domain.Task, agent domain.AgentDefinition, snapshot domain.ContextSnapshot) []gateway.Message {
	items := make([]domain.ContextSnapshotItem, len(snapshot.Items))
	copy(items, snapshot.Items)
	sort.Slice(items, func(i, j int) bool { return items[i].Order < items[j].Order })

	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = item.Name + ": " + item.Content
	}
	contextText := strings.Join(lines, "\n")

	return []gateway.Message{
		{Role: "system", Content: agent.Directive},
		{Role: "user", Content: contextText},
	}
}

// toolSpecsFor offers the Gateway only the tools the agent actually holds the capability for
// — an agent is never even shown a tool it has no chance of getting past PermissionPolicy's
// capability check, reducing (not eliminating — PermissionPolicy is still the real gate) the
// odds of a model asking for something it will just get denied.
func toolSpecsFor(agent domain.AgentDefinition, tools ToolRegistry) []gateway.ToolSpec {
	var specs []gateway.ToolSpec
	for _, capability := range agent.Capabilities {
		executor, ok := tools.Find(capability)
		if !ok {
			continue
		}
		def := executor.Definition()
		specs = append(specs, gateway.ToolSpec{Name: def.Name, Description: def.Description})
	}
	return specs
}
