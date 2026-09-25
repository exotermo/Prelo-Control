package br.com.exotermo.hermes.app.domain;
import java.util.List;
// A curated, configuration-defined agent — never built from request input. capabilities is
// empty in this iteration (no tools yet); modelProfile is resolved by the Gateway, never a raw
// provider/model chosen by the agent itself.
public record AgentDefinition(AgentId agentId, AgentType agentType, String version, String directive, String modelProfile, List<String> capabilities, String name, String description) {
 public AgentDefinition {
  if (agentId == null) throw new IllegalArgumentException("agentId is required");
  if (agentType == null) throw new IllegalArgumentException("agentType is required");
  if (version == null || version.isBlank()) throw new IllegalArgumentException("agent version is required");
  if (directive == null || directive.isBlank()) throw new IllegalArgumentException("agent directive is required");
  if (modelProfile == null || modelProfile.isBlank()) throw new IllegalArgumentException("agent modelProfile is required");
  capabilities = capabilities == null ? List.of() : List.copyOf(capabilities);
 }
}
