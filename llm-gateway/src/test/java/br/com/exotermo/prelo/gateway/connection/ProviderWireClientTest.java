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

    // --- Fase T: tool calling ---

    private static final com.fasterxml.jackson.databind.JsonNode SCHEMA;
    static {
        try {
            SCHEMA = new ObjectMapper().readTree("{\"type\":\"object\",\"properties\":{\"url\":{\"type\":\"string\"}},\"required\":[\"url\"]}");
        } catch (IOException e) { throw new IllegalStateException(e); }
    }
    private static final LLMRequest TOOL_REQUEST = new LLMRequest("general-chat", List.of(
        new LLMMessage("system", "s"), new LLMMessage("user", "veja o site"),
        new LLMMessage("assistant", "", "call_1", "inspect_website", "{\"url\":\"https://x.test\"}"),
        new LLMMessage("tool", "responde 200", "call_1", "inspect_website", null),
        new LLMMessage("user", "e agora?")),
        null, null, List.of(new LLMRequest.ToolSpec("inspect_website", "inspeciona", SCHEMA), new LLMRequest.ToolSpec("current_time", "hora")));

    @Test void anthropicGetsToolsAndToolTurnsAndAnswersWithAToolUse() throws Exception {
        String base = serve("/v1/messages", 200, "{\"model\":\"claude-x\",\"stop_reason\":\"tool_use\",\"content\":["
            + "{\"type\":\"text\",\"text\":\"vou ver\"},{\"type\":\"tool_use\",\"id\":\"toolu_9\",\"name\":\"current_time\",\"input\":{}}],"
            + "\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}");
        var response = client.chat("anthropic", base, "sk-ant-x", "claude-x", TOOL_REQUEST, 256, Duration.ofSeconds(5));
        assertEquals("TOOL_USE", response.kind());
        assertEquals("toolu_9", response.toolUseId());
        assertEquals("current_time", response.toolName());
        assertEquals("{}", response.toolArgsJson());
        var body = new ObjectMapper().readTree(seen.get("body"));
        assertEquals("inspect_website", body.path("tools").path(0).path("name").asText());
        assertEquals("url", body.path("tools").path(0).path("input_schema").path("required").path(0).asText());
        assertEquals("object", body.path("tools").path(1).path("input_schema").path("type").asText(), "no schema = empty object schema");
        var messages = body.path("messages");
        assertEquals("tool_use", messages.path(1).path("content").path(0).path("type").asText());
        assertEquals("https://x.test", messages.path(1).path("content").path(0).path("input").path("url").asText());
        assertEquals("user", messages.path(2).path("role").asText());
        assertEquals("tool_result", messages.path(2).path("content").path(0).path("type").asText());
        assertEquals("text", messages.path(2).path("content").path(1).path("type").asText(), "consecutive user turns are merged");
    }

    @Test void openAiGetsFunctionsAndToolMessagesAndAnswersWithAToolCall() throws Exception {
        String base = serve("/chat/completions", 200, "{\"model\":\"gpt-x\",\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":null,"
            + "\"tool_calls\":[{\"id\":\"call_7\",\"type\":\"function\",\"function\":{\"name\":\"inspect_website\",\"arguments\":\"{\\\"url\\\":\\\"https://y.test\\\"}\"}}]}}],"
            + "\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}");
        var response = client.chat("openai", base, "sk-1", "gpt-x", TOOL_REQUEST, 256, Duration.ofSeconds(5));
        assertEquals("TOOL_USE", response.kind());
        assertEquals("call_7", response.toolUseId());
        assertEquals("{\"url\":\"https://y.test\"}", response.toolArgsJson());
        var body = new ObjectMapper().readTree(seen.get("body"));
        assertTrue(body.has("max_completion_tokens") && !body.has("max_tokens"), "OpenAI needs max_completion_tokens");
        assertEquals("function", body.path("tools").path(0).path("type").asText());
        assertEquals("inspect_website", body.path("tools").path(0).path("function").path("name").asText());
        assertEquals("call_1", body.path("messages").path(2).path("tool_calls").path(0).path("id").asText());
        assertEquals("tool", body.path("messages").path(3).path("role").asText());
        assertEquals("call_1", body.path("messages").path(3).path("tool_call_id").asText());
    }

    @Test void aToolTheRequestDidNotOfferIsNeverReturnedAsAToolCall() throws Exception {
        String base = serve("/chat/completions", 200, "{\"choices\":[{\"message\":{\"content\":\"texto\",\"tool_calls\":[{\"id\":\"c\",\"function\":{\"name\":\"rm_rf\",\"arguments\":\"{}\"}}]}}]}");
        var response = client.chat("openai_compatible", base, null, "m", TOOL_REQUEST, 256, Duration.ofSeconds(5));
        assertEquals("FINAL", response.kind());
        assertTrue(new ObjectMapper().readTree(seen.get("body")).has("max_tokens"), "compatible services keep max_tokens");
    }
}
