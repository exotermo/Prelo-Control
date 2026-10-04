package br.com.exotermo.prelo.gateway.provider.anthropic;

import java.time.Duration;
import java.util.List;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.bind.DefaultValue;

@ConfigurationProperties("gateway.provider.anthropic")
public record AnthropicProviderProperties(
        boolean enabled,
        String baseUrl,
        String apiKeyFile,
        List<String> models,
        Duration timeout,
        @DefaultValue("1024") int maxTokens,
        String apiVersion) {
    public AnthropicProviderProperties {
        if (baseUrl == null || baseUrl.isBlank()) baseUrl = "https://api.anthropic.com";
        if (timeout == null) timeout = Duration.ofSeconds(30);
        if (apiVersion == null || apiVersion.isBlank()) apiVersion = "2023-06-01";
        models = models == null ? List.of() : List.copyOf(models);
    }

    // apiKeyFile is a filesystem path, not the secret itself, but it's still an operational
    // reference we don't want showing up in logs/actuator by accident — redacted defensively.
    @Override public String toString() {
        return "AnthropicProviderProperties{enabled=" + enabled + ", baseUrl=" + baseUrl + ", apiKeyFile=[redacted], models=" + models + ", timeout=" + timeout + ", maxTokens=" + maxTokens + ", apiVersion=" + apiVersion + "}";
    }
}
