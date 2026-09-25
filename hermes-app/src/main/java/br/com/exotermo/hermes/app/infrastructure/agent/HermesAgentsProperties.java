package br.com.exotermo.hermes.app.infrastructure.agent;
import java.util.List;
import java.util.Map;
import org.springframework.boot.context.properties.ConfigurationProperties;
@ConfigurationProperties("hermes.agents")
public record HermesAgentsProperties(Map<String, AgentConfig> catalog) {
 public record AgentConfig(String agentType, String version, String directive, String modelProfile, List<String> capabilities, String name, String description) { }
}
