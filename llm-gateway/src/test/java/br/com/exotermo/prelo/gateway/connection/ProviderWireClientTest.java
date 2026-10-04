package br.com.exotermo.prelo.gateway.connection;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.prelo.gateway.llm.LLMMessage;
import br.com.exotermo.prelo.gateway.llm.LLMRequest;
import br.com.exotermo.prelo.gateway.provider.ProviderAuthenticationException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class ProviderWireClientTest {
    private HttpServer server;
    private final Map<String, String> seen = new ConcurrentHashMap<>();

    @AfterEach void stop() { if (server != null) server.stop(0); }

    private String serve(String path, int status, String body) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext(path, exchange -> {
            seen.put("x-api-key", String.valueOf(exchange.getRequestHeaders().getFirst("x-api-key")));
            seen.put("authorization", String.valueOf(exchange.getRequestHeaders().getFirst("Authorization")));
            seen.put("body", new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8));
            byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(status, bytes.length);
            exchange.getResponseBody().write(bytes);
            exchange.close();
        });
        server.start();
        return "http://127.0.0.1:" + server.getAddress().getPort();
    }

    private final ProviderWireClient client = new ProviderWireClient(new ObjectMapper());
    private static final LLMRequest REQUEST = new LLMRequest("general-chat", List.of(new LLMMessage("system", "be brief"), new LLMMessage("user", "oi")), null, null, null);

    @Test void listsAnthropicModelsWithItsHeaders() throws Exception {
        String base = serve("/v1/models", 200, "{\"data\":[{\"id\":\"claude-b\"},{\"id\":\"claude-a\"}]}");
        assertEquals(List.of("claude-a", "claude-b"), client.listModels("anthropic", base, "sk-ant-x", Duration.ofSeconds(5)));
        assertEquals("sk-ant-x", seen.get("x-api-key"));
    }

    @Test void chatsWithOpenAiFormatAndBearerKey() throws Exception {
        String base = serve("/chat/completions", 200,
            "{\"model\":\"gpt-x\",\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"olá\"}}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}");
        var response = client.chat("openai", base, "sk-1", "gpt-x", REQUEST, 256, Duration.ofSeconds(5));
        assertEquals("olá", response.content());
        assertEquals(9, response.usage().totalTokens());
        assertEquals("Bearer sk-1", seen.get("authorization"));
        assertTrue(seen.get("body").contains("\"role\":\"system\""));
    }

    @Test void compatibleServiceWithoutKeySendsNoAuthorizationHeader() throws Exception {
        String base = serve("/models", 200, "{\"data\":[{\"id\":\"llama3.1:8b\"}]}");
        assertEquals(List.of("llama3.1:8b"), client.listModels("openai_compatible", base, null, Duration.ofSeconds(5)));
        assertEquals("null", seen.get("authorization"));
    }

    @Test void rejectedCredentialsBecomeAnAuthenticationFailureWithoutEchoingTheBody() throws Exception {
        String base = serve("/v1/models", 401, "{\"error\":{\"message\":\"invalid x-api-key sk-ant-x\"}}");
        var error = assertThrows(ProviderAuthenticationException.class, () -> client.listModels("anthropic", base, "sk-ant-x", Duration.ofSeconds(5)));
        assertFalse(error.getMessage().contains("sk-ant-x"));
    }
}
