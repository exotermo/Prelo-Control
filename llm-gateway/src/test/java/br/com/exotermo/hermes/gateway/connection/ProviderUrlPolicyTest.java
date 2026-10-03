package br.com.exotermo.hermes.gateway.connection;

import static org.junit.jupiter.api.Assertions.*;

import org.junit.jupiter.api.Test;

class ProviderUrlPolicyTest {
    @Test void firstPartyProvidersIgnoreTheRequestedUrl() {
        assertEquals("https://api.anthropic.com", ProviderUrlPolicy.resolveBaseUrl("anthropic", "http://evil.internal", false));
        assertEquals("https://api.openai.com/v1", ProviderUrlPolicy.resolveBaseUrl("openai", "http://127.0.0.1", false));
    }

    @Test void compatibleUrlsMustBeHttpsAndNotInternal() {
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("http://example.com/v1", false));
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("https://127.0.0.1/v1", false));
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("https://10.0.0.5/v1", false));
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("https://169.254.169.254/latest", false));
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("https://user:pw@example.com/v1", false));
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.validateCustom("ftp://example.com", false));
    }

    @Test void devFlagAllowsLocalServicesLikeOllama() {
        assertEquals("http://127.0.0.1:11434/v1", ProviderUrlPolicy.validateCustom("http://127.0.0.1:11434/v1/", true));
    }

    @Test void unknownProviderIsRefused() {
        assertThrows(ConnectionValidationException.class, () -> ProviderUrlPolicy.resolveBaseUrl("gemini", null, false));
    }
}
