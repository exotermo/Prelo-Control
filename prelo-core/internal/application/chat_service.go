package application

import (
	"context"

	"github.com/exotermo/prelo-core/internal/infrastructure/gateway"
)

// ChatService mirrors ChatService.java — a direct passthrough to the Gateway, bypassing
// Task/Execution persistence entirely, but auditing every call (success or failure) to
// llm_executions.
type ChatService struct {
	gateway LanguageModelGateway
	repo    LlmExecutionRepository
}

func NewChatService(gw LanguageModelGateway, repo LlmExecutionRepository) *ChatService {
	return &ChatService{gateway: gw, repo: repo}
}

func (s *ChatService) Chat(ctx context.Context, req gateway.ChatRequest, requestID, taskID, agentID string) (gateway.ChatResponse, error) {
	resp, err := s.gateway.Chat(ctx, req, requestID)
	if err != nil {
		s.audit(ctx, requestID, taskID, agentID, req.ModelProfile, nil, nil, "FAILED")
		return gateway.ChatResponse{}, err
	}
	provider := resp.Provider
	durationMs := resp.DurationMs
	s.audit(ctx, requestID, taskID, agentID, req.ModelProfile, &provider, &durationMs, "COMPLETED")
	return resp, nil
}

func (s *ChatService) audit(ctx context.Context, requestID, taskID, agentID, model string, provider *string, durationMs *int64, status string) {
	// Best-effort: mirrors the Java service, which lets the repository.save call fail loudly
	// (propagating as a 500) only on the success path; here we simply don't let audit failures
	// mask the primary Gateway result/error the caller is waiting on.
	_ = s.repo.Save(ctx, LlmExecutionRecord{
		RequestID: requestID, TaskID: taskID, AgentID: agentID, RequestedModel: model,
		Provider: provider, DurationMs: durationMs, Status: status,
	})
}
