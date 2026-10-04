package domain

import "testing"

func TestNewAgentDefinition_Valid(t *testing.T) {
	def, err := NewAgentDefinition(mustAgentID(t, "general"), AgentTypeGeneral, "1", "be helpful", "general-chat", nil, "General", "desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Capabilities == nil {
		t.Fatal("expected capabilities to default to empty slice, not nil")
	}
}

func TestNewAgentDefinition_RequiresVersionDirectiveModelProfile(t *testing.T) {
	agentID := mustAgentID(t, "general")

	if _, err := NewAgentDefinition(agentID, AgentTypeGeneral, "", "directive", "profile", nil, "", ""); err == nil {
		t.Fatal("expected error for blank version")
	}
	if _, err := NewAgentDefinition(agentID, AgentTypeGeneral, "1", "", "profile", nil, "", ""); err == nil {
		t.Fatal("expected error for blank directive")
	}
	if _, err := NewAgentDefinition(agentID, AgentTypeGeneral, "1", "directive", "", nil, "", ""); err == nil {
		t.Fatal("expected error for blank modelProfile")
	}
}
