package br.com.exotermo.hermes.gateway.routing;

import br.com.exotermo.hermes.gateway.llm.UnsupportedModelException;
import java.util.List;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.stereotype.Component;

// Resolves a caller-supplied modelProfile into the ordered, administered list of candidates
// configured under gateway.routing.profiles. Hermes never sends a fallback list itself — the
// Gateway alone decides candidate order and membership, from its own configuration.
@Component
@EnableConfigurationProperties(GatewayRoutingProperties.class)
public class FallbackPlanResolver {
    private final GatewayRoutingProperties properties;
    public FallbackPlanResolver(GatewayRoutingProperties properties) { this.properties = properties; }

    public List<ProviderCandidate> resolve(String profile) {
        if (properties.profiles() == null || !properties.profiles().containsKey(profile)) {
            throw new UnsupportedModelException(profile);
        }
        List<String> candidates = properties.profiles().get(profile).candidates();
        if (candidates == null || candidates.isEmpty()) throw new UnsupportedModelException(profile);
        return candidates.stream().map(ProviderCandidate::parse).toList();
    }

    public java.time.Duration totalBudget() { return properties.totalBudget(); }
    public boolean retryOnRateLimit() { return properties.retryOnRateLimit(); }
}
