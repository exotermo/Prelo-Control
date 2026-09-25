package domain

// DelegateToolName identifies the one tool the agent loop treats specially (Fase C): running it
// doesn't produce a result inline — it creates a child Task and suspends the calling execution
// until that child finishes (domain.SuspensionSubtask). Both RunAgentLoopUseCase and
// DecideApprovalUseCase check for this exact name; the tool's own definition
// (infrastructure/tools.DelegateTool) is the only place its behavior actually lives.
const DelegateToolName = "delegate_to_agent"

// ToolDefinition is a curated, descriptive record of an invocable tool — never built from
// request input, mirroring AgentDefinition's own curation story. The executable behavior
// deliberately does not live here: the domain package stays free of I/O, same as every other
// type in it; application.ToolExecutor is where a definition is paired with real code.
type ToolDefinition struct {
	Name        string
	Description string
	RiskLevel   RiskLevel
}

func NewToolDefinition(name, description string, riskLevel RiskLevel) (ToolDefinition, error) {
	if isBlank(name) {
		return ToolDefinition{}, &ValidationError{Message: "tool name is required"}
	}
	if isBlank(description) {
		return ToolDefinition{}, &ValidationError{Message: "tool description is required"}
	}
	if !riskLevel.Valid() {
		return ToolDefinition{}, &ValidationError{Message: "tool risk level is invalid"}
	}
	return ToolDefinition{Name: name, Description: description, RiskLevel: riskLevel}, nil
}
