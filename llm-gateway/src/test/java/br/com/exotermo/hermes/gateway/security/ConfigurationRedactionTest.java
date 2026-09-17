package br.com.exotermo.hermes.gateway.security;

import static org.junit.jupiter.api.Assertions.assertFalse;
import org.junit.jupiter.api.Test;

class ConfigurationRedactionTest {
    @Test void doesNotExposeTheJwtSecretThroughToString() {
        String secret = "gateway-secret-that-must-never-be-logged";
        assertFalse(new GatewayJwtProperties(secret, "issuer", "audience").toString().contains(secret));
    }
}
