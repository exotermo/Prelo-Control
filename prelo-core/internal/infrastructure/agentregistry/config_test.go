package agentregistry

import (
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
)

func TestLoadDefault_HasGeneralAndConcise(t *testing.T) {
	registry, err := LoadDefault()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	generalID, _ := domain.NewAgentID("general")
	def, ok := registry.Find(generalID)
	if !ok {
		t.Fatal("expected 'general' agent to exist")
	}
	if def.ModelProfile != "general-chat" {
		t.Fatalf("unexpected model profile: %s", def.ModelProfile)
	}

	conciseID, _ := domain.NewAgentID("concise")
	if _, ok := registry.Find(conciseID); !ok {
		t.Fatal("expected 'concise' agent to exist")
	}
}

func TestLoadDefault_UnknownAgent(t *testing.T) {
	registry, err := LoadDefault()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	missing, _ := domain.NewAgentID("does-not-exist")
	if _, err := registry.FindRequired(missing); err == nil {
		t.Fatal("expected an error for an unknown agent")
	}
}

func TestLoad_RequiresGeneralAgent(t *testing.T) {
	yaml := []byte("concise:\n  agent-type: GENERAL\n  version: \"1\"\n  directive: \"d\"\n  model-profile: p\n")
	if _, err := Load(yaml); err == nil {
		t.Fatal("expected an error when the catalog has no 'general' agent")
	}
}

func TestLoad_RejectsInvalidAgentType(t *testing.T) {
	yaml := []byte("general:\n  agent-type: BOGUS\n  version: \"1\"\n  directive: \"d\"\n  model-profile: p\n")
	if _, err := Load(yaml); err == nil {
		t.Fatal("expected an error for an invalid agent-type")
	}
}
