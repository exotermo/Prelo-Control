package gateway

// Wire types for llm-gateway's provider-neutral chat contract (POST /api/v1/llm/chat). The
// Gateway itself is Java/Spring and is NOT changing — these field names must match
// GatewayContracts.java exactly (Jackson serializes record components as-is, camelCase).

type ChatRequest struct {
	ModelProfile string            `json:"modelProfile"`
	Messages     []Message         `json:"messages"`
	Parameters   *Parameters       `json:"parameters"`
	Metadata     map[string]string `json:"metadata"`
	// Tools is deliberately minimal (name+description, no args schema) — see
	// LLMRequest.ToolSpec's doc on the Java side for why. Nil/empty means "no tools offered",
	// exactly like today.
	Tools []ToolSpec `json:"tools,omitempty"`
	// ProjectID (Fase M) lets the gateway pick the project's own model connection (else the
	// instance default). Empty for unassigned tasks.
	ProjectID string `json:"projectId,omitempty"`
}

type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Parameters struct {
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"maxTokens"`
}

const (
	KindFinal   = "FINAL"
	KindToolUse = "TOOL_USE"
)

// ChatResponse is a tagged union over Kind: FINAL carries Content, TOOL_USE carries
// ToolUseID/ToolName/ToolArgsJSON (Content empty). See LLMResponse.java for the Java side of
// this same contract.
type ChatResponse struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Kind         string `json:"kind"`
	Content      string `json:"content"`
	ToolUseID    string `json:"toolUseId"`
	ToolName     string `json:"toolName"`
	ToolArgsJSON string `json:"toolArgsJson"`
	Usage        Usage  `json:"usage"`
	DurationMs   int64  `json:"durationMs"`
	RequestID    string `json:"requestId"`
}

func (r ChatResponse) IsToolUse() bool { return r.Kind == KindToolUse }

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}
