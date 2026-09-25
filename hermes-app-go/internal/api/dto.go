package api

type createTaskRequest struct {
	Description string                     `json:"description"`
	Context     []manualContextItemRequest `json:"context"`
	AgentID     *string                    `json:"agentId"`
}

type manualContextItemRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type taskResponse struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	AgentID     string `json:"agentId"`
	CreatedAt   string `json:"createdAt"`
}

type executionResponse struct {
	ExecutionID string  `json:"executionId"`
	TaskID      string  `json:"taskId"`
	AgentID     string  `json:"agentId"`
	Status      string  `json:"status"`
	Result      *string `json:"result"`
	Error       *string `json:"error"`
	RequestID   *string `json:"requestId"`
	Model       *string `json:"model"`
	Provider    *string `json:"provider"`
	StartedAt   *string `json:"startedAt"`
	CompletedAt *string `json:"completedAt"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type chatRequest struct {
	Model      string          `json:"model"`
	Messages   []chatMessage   `json:"messages"`
	Parameters *chatParameters `json:"parameters"`
	TaskID     string          `json:"taskId"`
	AgentID    string          `json:"agentId"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatParameters struct {
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"maxTokens"`
}
