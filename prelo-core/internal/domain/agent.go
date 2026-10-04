package domain

type AgentType string

const AgentTypeGeneral AgentType = "GENERAL"

// AgentDefinition is a curated, configuration-defined agent — never built from request input.
// Capabilities defaults to empty (no tools yet); modelProfile is resolved by the Gateway, never
// a raw provider/model chosen by the agent itself.
type AgentDefinition struct {
	AgentID      AgentID
	AgentType    AgentType
	Version      string
	Directive    string
	ModelProfile string
	Capabilities []string
	Name         string
	Description  string
}

func NewAgentDefinition(agentID AgentID, agentType AgentType, version, directive, modelProfile string, capabilities []string, name, description string) (AgentDefinition, error) {
	if isBlank(version) {
		return AgentDefinition{}, &ValidationError{Message: "agent version is required"}
	}
	if isBlank(directive) {
		return AgentDefinition{}, &ValidationError{Message: "agent directive is required"}
	}
	if isBlank(modelProfile) {
		return AgentDefinition{}, &ValidationError{Message: "agent modelProfile is required"}
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	return AgentDefinition{
		AgentID:      agentID,
		AgentType:    agentType,
		Version:      version,
		Directive:    directive,
		ModelProfile: modelProfile,
		Capabilities: capabilities,
		Name:         name,
		Description:  description,
	}, nil
}
