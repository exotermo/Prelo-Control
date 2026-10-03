package api

import "github.com/exotermo/hermes-app-go/internal/domain"

// pipelineNodeResponse mirrors taskTreeNode (observability_dto.go) but replaces the raw
// Task/Execution status pair with a single human-facing pipelineStatus — the whole point of the
// Fase P Pipeline page is "what is this node actually waiting on right now", not the two
// separate state machines that produce that answer.
type pipelineNodeResponse struct {
	TaskID         string                 `json:"taskId"`
	Description    string                 `json:"description"`
	AgentID        string                 `json:"agentId"`
	Depth          int                    `json:"depth"`
	PipelineStatus string                 `json:"pipelineStatus"`
	Children       []pipelineNodeResponse `json:"children"`
}

func pipelineNodeFrom(t domain.Task, status string) pipelineNodeResponse {
	return pipelineNodeResponse{
		TaskID: t.ID.String(), Description: t.Description, AgentID: t.AgentID.String(),
		Depth: t.Depth, PipelineStatus: status, Children: []pipelineNodeResponse{},
	}
}
