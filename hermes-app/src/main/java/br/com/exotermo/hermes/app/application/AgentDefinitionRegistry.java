package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.AgentDefinition;
import br.com.exotermo.hermes.app.domain.AgentId;
import br.com.exotermo.hermes.app.domain.UnknownAgentException;
import java.util.Optional;
public interface AgentDefinitionRegistry {
 Optional<AgentDefinition> find(AgentId agentId);
 default AgentDefinition findRequired(AgentId agentId) { return find(agentId).orElseThrow(() -> new UnknownAgentException(agentId)); }
}
