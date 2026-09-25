package br.com.exotermo.hermes.gateway.routing;

import java.time.Duration;
import java.util.List;
import java.util.Map;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.bind.DefaultValue;

@ConfigurationProperties("gateway.routing")
public record GatewayRoutingProperties(Map<String, ProfileConfig> profiles, Duration totalBudget, @DefaultValue("true") boolean retryOnRateLimit) {
    public GatewayRoutingProperties {
        if (totalBudget == null) totalBudget = Duration.ofSeconds(20);
    }
    public record ProfileConfig(List<String> candidates) { }
}
