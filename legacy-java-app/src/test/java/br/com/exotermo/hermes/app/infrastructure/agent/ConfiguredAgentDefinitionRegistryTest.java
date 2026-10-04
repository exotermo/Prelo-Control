package br.com.exotermo.hermes.app.infrastructure.agent;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.AgentId;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class ConfiguredAgentDefinitionRegistryTest {

    private static HermesAgentsProperties.AgentConfig validConfig() {
        return new HermesAgentsProperties.AgentConfig("GENERAL", "1", "be helpful", "mock-echo", List.of(), "General", "desc");
    }

    @Test void resolvesAConfiguredAgentById() {
        var registry = new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", validConfig())));

        var definition = registry.findRequired(new AgentId("general"));

        assertEquals("general", definition.agentId().value());
        assertEquals("1", definition.version());
        assertEquals("mock-echo", definition.modelProfile());
        assertTrue(definition.capabilities().isEmpty());
    }

    @Test void twoAgentsCanShareAModelProfileWithDistinctDirectivesAndVersions() {
        var general = validConfig();
        var concise = new HermesAgentsProperties.AgentConfig("GENERAL", "1.1", "be terse", "mock-echo", List.of(), "Concise", "desc");
        var registry = new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", general, "concise", concise)));

        assertEquals("be helpful", registry.findRequired(new AgentId("general")).directive());
        assertEquals("be terse", registry.findRequired(new AgentId("concise")).directive());
        assertEquals("1", registry.findRequired(new AgentId("general")).version());
        assertEquals("1.1", registry.findRequired(new AgentId("concise")).version());
    }

    @Test void rejectsAnEmptyCatalog() {
        assertThrows(IllegalStateException.class, () -> new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of())));
    }

    @Test void requiresTheGeneralAgentToBePresent() {
        var registry = new HermesAgentsProperties(Map.of("concise", validConfig()));
        assertThrows(IllegalStateException.class, () -> new ConfiguredAgentDefinitionRegistry(registry));
    }

    @Test void rejectsAnAgentWithABlankDirective() {
        var config = new HermesAgentsProperties.AgentConfig("GENERAL", "1", "  ", "mock-echo", List.of(), "General", "desc");
        assertThrows(IllegalStateException.class, () -> new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", config))));
    }

    @Test void rejectsAnAgentWithABlankVersion() {
        var config = new HermesAgentsProperties.AgentConfig("GENERAL", "", "be helpful", "mock-echo", List.of(), "General", "desc");
        assertThrows(IllegalStateException.class, () -> new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", config))));
    }

    @Test void rejectsAnAgentWithoutAModelProfile() {
        var config = new HermesAgentsProperties.AgentConfig("GENERAL", "1", "be helpful", null, List.of(), "General", "desc");
        assertThrows(IllegalStateException.class, () -> new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", config))));
    }

    @Test void anUnknownAgentIdIsAbsent() {
        var registry = new ConfiguredAgentDefinitionRegistry(new HermesAgentsProperties(Map.of("general", validConfig())));
        assertTrue(registry.find(new AgentId("ghost")).isEmpty());
    }
}
