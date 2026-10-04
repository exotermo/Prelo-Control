package br.com.exotermo.prelo.gateway.provider.anthropic;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.prelo.gateway.llm.*;
import br.com.exotermo.prelo.gateway.provider.*;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class AnthropicProviderTest {

    private HttpServer server;

    @AfterEach void stopServer() {
        if (server != null) server.stop(0);
    }

    private String startServer(int statusCode, String responseBody, AtomicReference<String> capturedBody, AtomicReference<String> capturedApiKeyHeader, AtomicReference<String> capturedVersionHeader) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/v1/messages", exchange -> {
            capturedBody.set(new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8));
            capturedApiKeyHeader.set(exchange.getRequestHeaders().getFirst("x-api-key"));
            capturedVersionHeader.set(exchange.getRequestHeaders().getFirst("anthropic-version"));
            byte[] bytes = responseBody.getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(statusCode, bytes.length);
            exchange.getResponseBody().write(bytes);
            exchange.close();
        });
        server.start();
        return "http://127.0.0.1:" + server.getAddress().getPort();
    }

    private String startServer(int statusCode, String responseBody) throws IOException {
        return startServer(statusCode, responseBody, new AtomicReference<>(), new AtomicReference<>(), new AtomicReference<>());
    }

    private AnthropicProvider provider(String baseUrl, Path keyFile, Duration timeout) throws IOException {
        Files.writeString(keyFile, "test-anthropic-key");
        AnthropicProviderProperties properties = new AnthropicProviderProperties(true, baseUrl, keyFile.toString(), List.of("claude-3-5-sonnet-latest"), timeout, 1024, "2023-06-01");
        return new AnthropicProvider(properties, new ProviderSecretLoader(), new ObjectMapper());
    }

    @Test void sendsACorrectlyFormedRequestWithSystemMessageSeparatedFromMessages(@TempDir Path tempDir) throws Exception {
        AtomicReference<String> body = new AtomicReference<>();
        AtomicReference<String> apiKeyHeader = new AtomicReference<>();
        AtomicReference<String> versionHeader = new AtomicReference<>();
        String baseUrl = startServer(200, """
            {"id":"msg_1","model":"claude-3-5-sonnet-latest","content":[{"type":"text","text":"hi there"}],"usage":{"input_tokens":5,"output_tokens":3}}
            """, body, apiKeyHeader, versionHeader);

        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("system", "be terse"), new LLMMessage("user", "hello")), null, null, null);

        LLMResponse response = provider.execute("claude-3-5-sonnet-latest", request);

        assertEquals("hi there", response.content());
        assertEquals("anthropic", response.provider());
        assertEquals("claude-3-5-sonnet-latest", response.model());
        assertEquals(5, response.usage().inputTokens());
        assertEquals(3, response.usage().outputTokens());
        assertEquals("test-anthropic-key", apiKeyHeader.get());
        assertEquals("2023-06-01", versionHeader.get());
        assertTrue(body.get().contains("\"system\":\"be terse\""), "system message must be lifted to the top-level 'system' field");
        assertFalse(body.get().contains("\"role\":\"system\""), "system message must not appear inside the messages array");
        assertTrue(body.get().contains("\"role\":\"user\""));
    }

    @Test void mapsATimeoutToProviderTimeoutException(@TempDir Path tempDir) throws Exception {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/v1/messages", exchange -> {
            try { Thread.sleep(3000); } catch (InterruptedException ignored) { Thread.currentThread().interrupt(); }
        });
        server.start();
        String baseUrl = "http://127.0.0.1:" + server.getAddress().getPort();

        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofMillis(300));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);

        assertThrows(ProviderTimeoutException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void maps401ToProviderAuthenticationException(@TempDir Path tempDir) throws Exception {
        String baseUrl = startServer(401, "{\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid key\"}}");
        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);
        assertThrows(ProviderAuthenticationException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void maps403ToProviderAuthenticationException(@TempDir Path tempDir) throws Exception {
        String baseUrl = startServer(403, "{\"error\":{\"type\":\"permission_error\",\"message\":\"forbidden\"}}");
        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);
        assertThrows(ProviderAuthenticationException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void maps429ToProviderRateLimitedException(@TempDir Path tempDir) throws Exception {
        String baseUrl = startServer(429, "{\"error\":{\"type\":\"rate_limit_error\",\"message\":\"slow down\"}}");
        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);
        assertThrows(ProviderRateLimitedException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void maps5xxToProviderUnavailableException(@TempDir Path tempDir) throws Exception {
        String baseUrl = startServer(503, "{\"error\":{\"type\":\"overloaded_error\",\"message\":\"try later\"}}");
        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);
        assertThrows(ProviderUnavailableException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void maps400ToProviderRejectedRequestException(@TempDir Path tempDir) throws Exception {
        String baseUrl = startServer(400, "{\"error\":{\"type\":\"invalid_request_error\",\"message\":\"bad request\"}}");
        AnthropicProvider provider = provider(baseUrl, tempDir.resolve("key.txt"), Duration.ofSeconds(5));
        LLMRequest request = new LLMRequest("general-chat", List.of(new LLMMessage("user", "hello")), null, null, null);
        assertThrows(ProviderRejectedRequestException.class, () -> provider.execute("claude-3-5-sonnet-latest", request));
    }

    @Test void failsAtConstructionWhenTheApiKeyFileIsMissing(@TempDir Path tempDir) {
        AnthropicProviderProperties properties = new AnthropicProviderProperties(true, "http://localhost:0", tempDir.resolve("missing.txt").toString(), List.of("claude-3-5-sonnet-latest"), Duration.ofSeconds(5), 1024, "2023-06-01");
        assertThrows(IllegalStateException.class, () -> new AnthropicProvider(properties, new ProviderSecretLoader(), new ObjectMapper()));
    }

    @Test void failsAtConstructionWhenTheApiKeyFileIsEmpty(@TempDir Path tempDir) throws IOException {
        Path keyFile = tempDir.resolve("empty.txt");
        Files.writeString(keyFile, "   ");
        AnthropicProviderProperties properties = new AnthropicProviderProperties(true, "http://localhost:0", keyFile.toString(), List.of("claude-3-5-sonnet-latest"), Duration.ofSeconds(5), 1024, "2023-06-01");
        assertThrows(IllegalStateException.class, () -> new AnthropicProvider(properties, new ProviderSecretLoader(), new ObjectMapper()));
    }

    @Test void supportsOnlyExplicitlyConfiguredModels(@TempDir Path tempDir) throws IOException {
        Path keyFile = tempDir.resolve("key.txt");
        Files.writeString(keyFile, "test-key");
        AnthropicProviderProperties properties = new AnthropicProviderProperties(true, "http://localhost:0", keyFile.toString(), List.of("claude-3-5-sonnet-latest"), Duration.ofSeconds(5), 1024, "2023-06-01");
        AnthropicProvider provider = new AnthropicProvider(properties, new ProviderSecretLoader(), new ObjectMapper());
        assertTrue(provider.supports("claude-3-5-sonnet-latest"));
        assertFalse(provider.supports("gpt-4"));
    }
}
