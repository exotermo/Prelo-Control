package dev.hermes.bridge;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.HexFormat;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.*;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;

/**
 * End to end: a signed inbound_message webhook drives a real (mocked-downstream) round trip —
 * this bridge creates+executes a task on a fake hermes-app-go, polls it to COMPLETED, then
 * posts the result to a fake messaging-core /api/v1/messages.
 *
 * How to run: `mvn verify` (no Docker needed — both downstreams are plain local HttpServers).
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class BridgeFlowIT {

    private static final String SECRET = "webhook-test-secret";
    private static HttpServer hermesAppServer;
    private static HttpServer messagingCoreServer;
    private static int hermesAppPort;
    private static int messagingCorePort;

    @DynamicPropertySource
    static void properties(DynamicPropertyRegistry registry) throws Exception {
        hermesAppServer = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        hermesAppPort = hermesAppServer.getAddress().getPort();
        messagingCoreServer = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        messagingCorePort = messagingCoreServer.getAddress().getPort();

        registry.add("bridge.hermes-app-url", () -> "http://127.0.0.1:" + hermesAppPort);
        registry.add("bridge.messaging-core-url", () -> "http://127.0.0.1:" + messagingCorePort);
        registry.add("bridge.messaging-core-client-id", () -> "test-client");
        registry.add("bridge.messaging-core-client-secret", () -> "test-secret");
        registry.add("bridge.callback-signing-secret", () -> SECRET);
        registry.add("bridge.auto-reply-enabled", () -> true);
        registry.add("bridge.admin-token", () -> "admin-test-token");
    }

    @AfterEach
    void stopServers() {
        if (hermesAppServer != null) hermesAppServer.stop(0);
        if (messagingCoreServer != null) messagingCoreServer.stop(0);
    }

    @Autowired private TestRestTemplate restTemplate;
    private final ObjectMapper objectMapper = new ObjectMapper();

    @Test
    void aSignedInboundEventProducesAReplyThroughMessagingCore() throws Exception {
        AtomicInteger executeCalls = new AtomicInteger(0);

        hermesAppServer.createContext("/api/v1/tasks", exchange -> {
            if ("POST".equals(exchange.getRequestMethod()) && !exchange.getRequestURI().getPath().contains("/execute")) {
                respondJson(exchange, 201, Map.of("id", "task-1", "description", "oi", "status", "PENDING", "agentId", "general"));
                return;
            }
            respondJson(exchange, 404, Map.of());
        });
        hermesAppServer.createContext("/api/v1/tasks/task-1/execute", exchange -> {
            executeCalls.incrementAndGet();
            respondJson(exchange, 202, Map.of("executionId", "exec-1", "taskId", "task-1", "status", "PENDING"));
        });
        hermesAppServer.createContext("/api/v1/tasks/task-1/executions/exec-1", exchange ->
            respondJson(exchange, 200, Map.of("status", "COMPLETED", "result", "Oi! Tudo bem?", "error", null)));
        hermesAppServer.start();

        BlockingQueue<String> messagingCoreBodies = new ArrayBlockingQueue<>(1);
        messagingCoreServer.createContext("/oauth/token", exchange ->
            respondJson(exchange, 200, Map.of("access_token", "fake-token", "token_type", "Bearer", "expires_in", 900)));
        messagingCoreServer.createContext("/api/v1/messages", exchange -> {
            String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            messagingCoreBodies.offer(body);
            respondJson(exchange, 202, Map.of("id", "msg-1", "status", "QUEUED", "externalMessageId", null));
        });
        messagingCoreServer.start();

        String eventBody = objectMapper.writeValueAsString(Map.of(
            "eventType", "inbound_message",
            "channelIdentityId", "channel-1",
            "conversationId", "conv-1",
            "messageId", "msg-inbound-1",
            "externalMessageId", "wamid-1",
            "from", "+5511888888888",
            "text", "oi",
            "receivedAt", "2026-09-22T12:00:00Z"
        ));

        HttpHeaders headers = new HttpHeaders();
        headers.setContentType(MediaType.APPLICATION_JSON);
        String timestamp = Long.toString(Instant.now().getEpochSecond());
        headers.set("X-Webhook-Timestamp", timestamp);
        headers.set("X-Webhook-Id", "webhook-test-1");
        headers.set("X-Request-Id", "request-test-1");
        headers.set("X-Signature", "sha256=" + hmacHex(SECRET, timestamp + "." + eventBody));

        var response = restTemplate.exchange("/webhooks/messaging-core", HttpMethod.POST,
            new HttpEntity<>(eventBody, headers), Void.class);
        assertEquals(HttpStatus.ACCEPTED, response.getStatusCode());

        String sentBody = messagingCoreBodies.poll(10, TimeUnit.SECONDS);
        assertNotNull(sentBody, "expected the bridge to reply through messaging-core");
        assertTrue(sentBody.contains("\"channelIdentityId\":\"channel-1\""));
        assertTrue(sentBody.contains("\"to\":\"+5511888888888\""));
        assertTrue(sentBody.contains("\"text\":\"Oi! Tudo bem?\""));
        assertEquals(1, executeCalls.get());
    }

    @Test
    void anUnsignedEventIsRejected() {
        HttpHeaders headers = new HttpHeaders();
        headers.setContentType(MediaType.APPLICATION_JSON);
        var response = restTemplate.exchange("/webhooks/messaging-core", HttpMethod.POST,
            new HttpEntity<>("{}", headers), Void.class);
        assertEquals(HttpStatus.UNAUTHORIZED, response.getStatusCode());
    }

    private static void respondJson(com.sun.net.httpserver.HttpExchange exchange, int status, Map<?, ?> body) {
        try {
            byte[] bytes = new ObjectMapper().writeValueAsBytes(body);
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(status, bytes.length);
            exchange.getResponseBody().write(bytes);
            exchange.close();
        } catch (Exception exception) {
            throw new RuntimeException(exception);
        }
    }

    private static String hmacHex(String secret, String body) throws Exception {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        return HexFormat.of().formatHex(mac.doFinal(body.getBytes(StandardCharsets.UTF_8)));
    }
}
