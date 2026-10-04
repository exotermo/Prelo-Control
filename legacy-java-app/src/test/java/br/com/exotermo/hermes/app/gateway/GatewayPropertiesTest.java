package br.com.exotermo.hermes.app.gateway;

import static org.junit.jupiter.api.Assertions.assertFalse;
import org.junit.jupiter.api.Test;

class GatewayPropertiesTest {
    @Test void doesNotExposeTheGatewayJwtSecretThroughToString() {
        String secret = "hermes-gateway-secret-that-must-never-be-logged";
        assertFalse(new GatewayProperties("http://gateway", secret, "issuer", "audience").toString().contains(secret));
    }
}
