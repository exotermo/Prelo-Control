package api

import (
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

type turnResponse struct {
	TurnNumber  int                `json:"turnNumber"`
	Kind        string             `json:"kind"`
	RequestID   string             `json:"requestId"`
	Input       string             `json:"input"`
	Output      *string            `json:"output"`
	Error       *string            `json:"error"`
	StartedAt   string             `json:"startedAt"`
	CompletedAt *string            `json:"completedAt"`
	Usage       *turnUsageResponse `json:"usage,omitempty"`
}

type turnUsageResponse struct {
	ModelProfile           string `json:"modelProfile"`
	TaskKind               string `json:"taskKind"`
	EstimatedContextTokens int    `json:"estimatedContextTokens"`
	InputTokens            int    `json:"inputTokens"`
	OutputTokens           int    `json:"outputTokens"`
	DurationMs             int64  `json:"durationMs"`
}

func turnResponseFrom(t domain.ExecutionTurn) turnResponse {
	resp := turnResponse{
		TurnNumber: t.TurnNumber, Kind: string(t.Kind), RequestID: t.RequestID,
		Input: t.Input, Output: t.Output, Error: t.Error, StartedAt: t.StartedAt.Format(time.RFC3339),
	}
	if t.CompletedAt != nil {
		c := t.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &c
	}
	if t.Kind == domain.TurnLLMCall && t.ModelProfile != "" {
		resp.Usage = &turnUsageResponse{ModelProfile: t.ModelProfile, TaskKind: t.TaskKind, EstimatedContextTokens: t.EstimatedContextTokens,
			InputTokens: t.InputTokens, OutputTokens: t.OutputTokens, DurationMs: t.DurationMs}
	}
	return resp
}

type taskTreeNode struct {
	TaskID          string         `json:"taskId"`
	Description     string         `json:"description"`
	Status          string         `json:"status"`
	AgentID         string         `json:"agentId"`
	Depth           int            `json:"depth"`
	ExecutionStatus *string        `json:"executionStatus"`
	Children        []taskTreeNode `json:"children"`
}

func taskTreeNodeFrom(t domain.Task) taskTreeNode {
	return taskTreeNode{
		TaskID: t.ID.String(), Description: t.Description, Status: string(t.Status),
		AgentID: t.AgentID.String(), Depth: t.Depth, Children: []taskTreeNode{},
	}
}
