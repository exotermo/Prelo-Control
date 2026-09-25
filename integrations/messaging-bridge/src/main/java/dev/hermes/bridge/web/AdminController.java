package dev.hermes.bridge.web;

import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.service.AutoReplyGate;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class AdminController {
    private final BridgeProperties properties;
    private final AutoReplyGate gate;

    public AdminController(BridgeProperties properties, AutoReplyGate gate) {
        this.properties = properties;
        this.gate = gate;
    }

    @PostMapping("/admin/pause")
    public ResponseEntity<AutoReplyStatus> pause(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        gate.pause();
        return ResponseEntity.ok(status());
    }

    @PostMapping("/admin/resume")
    public ResponseEntity<AutoReplyStatus> resume(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        gate.resume();
        return ResponseEntity.ok(status());
    }

    private void requireAdminToken(String provided) {
        String expected = properties.adminToken();
        if (provided == null || expected == null || expected.isBlank()
            || !MessageDigest.isEqual(provided.getBytes(StandardCharsets.UTF_8), expected.getBytes(StandardCharsets.UTF_8))) {
            throw new org.springframework.web.server.ResponseStatusException(HttpStatus.UNAUTHORIZED, "invalid admin token");
        }
    }

    private AutoReplyStatus status() {
        return new AutoReplyStatus(properties.autoReplyEnabled(), gate.isRuntimeEnabled(), gate.isEnabled());
    }

    public record AutoReplyStatus(boolean staticEnabled, boolean runtimeEnabled, boolean effectiveEnabled) { }
}
