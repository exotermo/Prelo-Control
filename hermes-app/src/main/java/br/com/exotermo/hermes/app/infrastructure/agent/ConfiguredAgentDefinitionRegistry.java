package br.com.exotermo.hermes.app.infrastructure.agent;
import br.com.exotermo.hermes.app.application.AgentDefinitionRegistry;
import br.com.exotermo.hermes.app.domain.*;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.stereotype.Component;

// The whole catalog is validated once, at startup: an invalid or incomplete entry fails
// application boot loudly instead of surfacing as a runtime UnknownAgentException later.
@Component
@EnableConfigurationProperties(HermesAgentsProperties.class)
public class ConfiguredAgentDefinitionRegistry implements AgentDefinitionRegistry {
 private final Map<String, AgentDefinition> definitions = new HashMap<>();

 public ConfiguredAgentDefinitionRegistry(HermesAgentsProperties properties) {
  if (properties.catalog() == null || properties.catalog().isEmpty()) {
   throw new IllegalStateException("hermes.agents.catalog must define at least the 'general' agent");
  }
  properties.catalog().forEach((id, config) -> {
   if (id == null || id.isBlank()) throw new IllegalStateException("hermes.agents.catalog entries must have a non-blank id");
   AgentType agentType;
   try { agentType = AgentType.valueOf(config.agentType()); }
   catch (RuntimeException e) { throw new IllegalStateException("hermes.agents.catalog." + id + ".agent-type is invalid: " + config.agentType(), e); }
   List<String> capabilities = config.capabilities() == null ? List.of() : config.capabilities();
   AgentDefinition definition;
   try {
    definition = new AgentDefinition(new AgentId(id), agentType, config.version(), config.directive(), config.modelProfile(), capabilities, config.name() == null ? id : config.name(), config.description());
   } catch (IllegalArgumentException e) {
    throw new IllegalStateException("hermes.agents.catalog." + id + " is invalid: " + e.getMessage(), e);
   }
   definitions.put(id, definition);
  });
  if (!definitions.containsKey("general")) throw new IllegalStateException("hermes.agents.catalog must define the 'general' agent");
 }

 public Optional<AgentDefinition> find(AgentId agentId) { return Optional.ofNullable(definitions.get(agentId.value())); }
}
