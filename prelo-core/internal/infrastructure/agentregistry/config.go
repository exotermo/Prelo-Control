package agentregistry

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

//go:embed catalog.yaml
var defaultCatalogYAML []byte

type agentConfig struct {
	AgentType    string   `yaml:"agent-type"`
	Version      string   `yaml:"version"`
	Directive    string   `yaml:"directive"`
	ModelProfile string   `yaml:"model-profile"`
	Capabilities []string `yaml:"capabilities"`
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
}

// LoadDefault builds the registry from the embedded catalog.yaml, matching the Java
// service's application.yml `prelo.agents.catalog` (general + concise).
func LoadDefault() (application.AgentRegistry, error) {
	return Load(defaultCatalogYAML)
}

// Load validates the whole catalog once, at startup: an invalid or incomplete entry fails
// loudly here instead of surfacing as a runtime unknown-agent error later, matching
// ConfiguredAgentDefinitionRegistry.java.
func Load(catalogYAML []byte) (*Registry, error) {
	var raw map[string]agentConfig
	if err := yaml.Unmarshal(catalogYAML, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse agent catalog: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("agent catalog must define at least the 'general' agent")
	}

	definitions := make(map[string]domain.AgentDefinition, len(raw))
	for id, cfg := range raw {
		if isBlank(id) {
			return nil, fmt.Errorf("agent catalog entries must have a non-blank id")
		}
		if cfg.AgentType != string(domain.AgentTypeGeneral) {
			return nil, fmt.Errorf("prelo.agents.catalog.%s.agent-type is invalid: %s", id, cfg.AgentType)
		}
		name := cfg.Name
		if name == "" {
			name = id
		}
		agentID, err := domain.NewAgentID(id)
		if err != nil {
			return nil, fmt.Errorf("prelo.agents.catalog.%s is invalid: %w", id, err)
		}
		def, err := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, cfg.Version, cfg.Directive, cfg.ModelProfile, cfg.Capabilities, name, cfg.Description)
		if err != nil {
			return nil, fmt.Errorf("prelo.agents.catalog.%s is invalid: %w", id, err)
		}
		definitions[id] = def
	}
	if _, ok := definitions["general"]; !ok {
		return nil, fmt.Errorf("agent catalog must define the 'general' agent")
	}
	return &Registry{definitions: definitions}, nil
}

func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}
