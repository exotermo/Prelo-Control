package api

import (
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

type toolResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RiskLevel   string `json:"riskLevel"`
}

func toolResponseFrom(def domain.ToolDefinition) toolResponse {
	return toolResponse{Name: def.Name, Description: def.Description, RiskLevel: string(def.RiskLevel)}
}

type invokeToolRequest struct {
	Args string `json:"args"`
}

type toolCallResponse struct {
	ID          string  `json:"id"`
	TaskID      string  `json:"taskId"`
	ExecutionID string  `json:"executionId"`
	AgentID     string  `json:"agentId"`
	ToolName    string  `json:"toolName"`
	RiskLevel   string  `json:"riskLevel"`
	Decision    string  `json:"decision"`
	Outcome     *string `json:"outcome"`
	Result      *string `json:"result"`
	Error       *string `json:"error"`
	CreatedAt   string  `json:"createdAt"`
	ResolvedAt  *string `json:"resolvedAt"`
}

func toolCallResponseFrom(c domain.ToolCall) toolCallResponse {
	resp := toolCallResponse{
		ID: c.ID.String(), TaskID: c.TaskID.String(), ExecutionID: c.ExecutionID.String(),
		AgentID: c.AgentID.String(), ToolName: c.ToolName, RiskLevel: string(c.RiskLevel),
		Decision: string(c.Decision), Result: c.Result, Error: c.Error,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
	if c.Outcome != nil {
		o := string(*c.Outcome)
		resp.Outcome = &o
	}
	if c.ResolvedAt != nil {
		r := c.ResolvedAt.Format(time.RFC3339)
		resp.ResolvedAt = &r
	}
	return resp
}

type decideApprovalRequest struct {
	DecidedBy string `json:"decidedBy"`
}

type approvalResponse struct {
	ID          string  `json:"id"`
	ToolCallID  string  `json:"toolCallId"`
	Scope       string  `json:"scope"`
	Status      string  `json:"status"`
	RequestedAt string  `json:"requestedAt"`
	ExpiresAt   string  `json:"expiresAt"`
	DecidedAt   *string `json:"decidedAt"`
	DecidedBy   *string `json:"decidedBy"`
}

func approvalResponseFrom(a domain.ApprovalRequest) approvalResponse {
	resp := approvalResponse{
		ID: a.ID.String(), ToolCallID: a.ToolCallID.String(), Scope: a.Scope,
		Status:      string(a.EffectiveStatus(time.Now().UTC())),
		RequestedAt: a.RequestedAt.Format(time.RFC3339),
		ExpiresAt:   a.ExpiresAt.Format(time.RFC3339),
		DecidedBy:   a.DecidedBy,
	}
	if a.DecidedAt != nil {
		d := a.DecidedAt.Format(time.RFC3339)
		resp.DecidedAt = &d
	}
	return resp
}
