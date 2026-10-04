package br.com.exotermo.prelo.gateway.security;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;
import br.com.exotermo.prelo.gateway.provider.anthropic.AnthropicProviderProperties;
import java.time.Duration;
import java.util.List;
import org.junit.jupiter.api.Test;

class ConfigurationRedactionTest {
    @Test void doesNotExposeTheJwtSecretThroughToString() {
        String secret = "gateway-secret-that-must-never-be-logged";
        assertFalse(new GatewayJwtProperties(secret, "issuer", "audience").toString().contains(secret));
    }

    @Test void doesNotExposeTheAnthropicApiKeyFilePathThroughToString() {
        String sensitivePath = "/run/secrets/anthropic_api_key";
        String text = new AnthropicProviderProperties(true, "https://api.anthropic.com", sensitivePath, List.of("claude-3-5-sonnet"), Duration.ofSeconds(30), 1024, "2023-06-01").toString();
        assertFalse(text.contains(sensitivePath));
        assertTrue(text.contains("[redacted]"));
    }
}
