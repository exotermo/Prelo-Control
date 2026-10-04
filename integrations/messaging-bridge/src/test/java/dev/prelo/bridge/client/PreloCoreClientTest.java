package dev.prelo.bridge.client;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import dev.prelo.bridge.config.BridgeProperties;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Base64;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

// Regression test for the 2026-10-01 incident: PreloCoreClient used to present
// MessagingCoreClient's token to prelo-core, which carries messaging-core's own scopes
// (messages:send/channels:manage/callbacks:manage) and prelo-core rejected it with 403 the first
// time a real inbound WhatsApp message exercised this path. It now mints its own token locally
// (PreloCoreJwt) — this verifies that token actually carries what prelo-core requires.
class PreloCoreClientTest {
    private HttpServer server;
    private final AtomicReference<String> capturedAuthHeader = new AtomicReference<>();
    private final AtomicReference<String> capturedCreateBody = new AtomicReference<>();

    @BeforeEach
    void start() throws Exception {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/v1/tasks", exchange -> {
            if ("POST".equals(exchange.getRequestMethod()) && exchange.getRequestURI().getPath().equals("/api/v1/tasks")) {
                capturedAuthHeader.set(exchange.getRequestHeaders().getFirst("Authorization"));
                capturedCreateBody.set(new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8));
                respond(exchange, "{\"id\":\"task-1\"}");
            } else if (exchange.getRequestURI().getPath().endsWith("/execute")) {
                respond(exchange, "{\"executionId\":\"exec-1\"}");
            } else {
                respond(exchange, "{\"status\":\"COMPLETED\",\"result\":\"[mock] hi\"}");
            }
        });
        server.start();
    }

    @AfterEach
    void stop() {
        server.stop(0);
    }

    @Test
    void run_presentsALocallyMintedTokenWithTheScopesPreloCoreRequires() {
        String baseUrl = "http://127.0.0.1:" + server.getAddress().getPort();
        BridgeProperties properties = new BridgeProperties("", "", "", "", baseUrl, true, "admin-token", List.of(),
            300, true, "shared-secret", "messaging-core", "messaging-core");
        PreloCoreClient client = new PreloCoreClient(properties);

        PreloCoreClient.ExecutionResult result = client.run("hi", "general", Duration.ofSeconds(5));

        assertEquals("COMPLETED", result.status());
        assertEquals("[mock] hi", result.result());
        String auth = capturedAuthHeader.get();
        assertTrue(auth != null && auth.startsWith("Bearer "), "expected a Bearer token, got: " + auth);
        String payload = decodePayload(auth.substring("Bearer ".length()));
        assertTrue(payload.contains("\"tasks:create\""), "token must carry tasks:create: " + payload);
        assertTrue(payload.contains("\"tasks:execute\""), "token must carry tasks:execute: " + payload);
        assertTrue(payload.contains("\"tasks:read\""), "token must carry tasks:read: " + payload);
        assertTrue(payload.contains("\"token_use\":\"technical\""), "token must be token_use=technical: " + payload);
        // Regression test for 2026-10-02: every inbound WhatsApp message was showing up in
        // prelo-dashboard's Tasks list indistinguishable from a task someone actually
        // designated — the bridge must tag every task it creates as MESSAGING.
        assertTrue(capturedCreateBody.get().contains("\"source\":\"MESSAGING\""),
            "task creation must be tagged source=MESSAGING: " + capturedCreateBody.get());
    }

    private static String decodePayload(String jwt) {
        String[] parts = jwt.split("\\.");
        return new String(Base64.getUrlDecoder().decode(parts[1]), StandardCharsets.UTF_8);
    }

    private static void respond(com.sun.net.httpserver.HttpExchange exchange, String body) throws java.io.IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().add("Content-Type", "application/json");
        exchange.sendResponseHeaders(200, bytes.length);
        try (var os = exchange.getResponseBody()) {
            os.write(bytes);
        }
    }
}
