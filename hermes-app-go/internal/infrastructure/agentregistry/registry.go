package agentregistry

import (
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type Registry struct {
	definitions map[string]domain.AgentDefinition
}

func (r *Registry) Find(agentID domain.AgentID) (domain.AgentDefinition, bool) {
	def, ok := r.definitions[agentID.Value]
	return def, ok
}

func (r *Registry) FindRequired(agentID domain.AgentID) (domain.AgentDefinition, error) {
	def, ok := r.Find(agentID)
	if !ok {
		return domain.AgentDefinition{}, &domain.ErrUnknownAgent{AgentID: agentID.Value}
	}
	return def, nil
}
