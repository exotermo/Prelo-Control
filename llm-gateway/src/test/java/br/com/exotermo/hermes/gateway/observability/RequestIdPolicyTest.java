package br.com.exotermo.hermes.gateway.observability;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import org.junit.jupiter.api.Test;
import org.springframework.web.server.ResponseStatusException;

class RequestIdPolicyTest {
    @Test void generatesAnIdWhenTheHeaderIsAbsent() {
        String requestId = RequestIdPolicy.resolve(null);
        assertNotNull(requestId);
        assertEquals(36, requestId.length());
    }

    @Test void preservesAValidRequestId() {
        assertEquals("task-42.agent_7", RequestIdPolicy.resolve("task-42.agent_7"));
    }

    @Test void rejectsAnEmptyOrOversizedRequestId() {
        assertThrows(ResponseStatusException.class, () -> RequestIdPolicy.resolve(""));
        assertThrows(ResponseStatusException.class, () -> RequestIdPolicy.resolve("x".repeat(101)));
    }
}
