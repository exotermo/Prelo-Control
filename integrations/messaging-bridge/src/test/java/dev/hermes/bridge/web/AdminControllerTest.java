package dev.hermes.bridge.web;

import static org.junit.jupiter.api.Assertions.assertEquals;

import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.service.AutoReplyGate;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;

class AdminControllerTest {
    @Test
    void pauseImmediatelyDisablesAStaticallyEnabledBridge() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", java.util.List.of());
        AutoReplyGate gate = new AutoReplyGate(properties);
        AdminController controller = new AdminController(properties, gate);

        assertEquals(HttpStatus.OK, controller.pause("admin-token").getStatusCode());
        assertEquals(false, gate.isEnabled());
        assertEquals(HttpStatus.OK, controller.resume("admin-token").getStatusCode());
        assertEquals(true, gate.isEnabled());
    }
}
