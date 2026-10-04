package br.com.exotermo.prelo.gateway.connection;

import br.com.exotermo.prelo.gateway.llm.LLMMessage;
import br.com.exotermo.prelo.gateway.llm.LLMRequest;
import br.com.exotermo.prelo.gateway.llm.LLMResponse;
import br.com.exotermo.prelo.gateway.provider.*;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.stream.Collectors;

/**
 * Talks to a provider with a key supplied per call (decrypted from the vault just for this call),
 * in either wire format: Anthropic Messages API, or OpenAI Chat Completions (OpenAI itself and
 * every OpenAI-compatible service). Error mapping matches AnthropicProvider: by status class only,
 * never echoing upstream bodies or headers. Tool calls (Fase T) are translated both ways: the
 * provider-neutral LLMMessage tool fields become Anthropic tool_use/tool_result blocks or OpenAI
 * tool_calls/role:tool messages, and the provider's first tool call comes back as KIND_TOOL_USE.
 */
class ProviderWireClient {
    private static final String ANTHROPIC_VERSION = "2023-06-01";

    private final HttpClient http;
    private final ObjectMapper json;

    ProviderWireClient(ObjectMapper json) {
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10))
            .followRedirects(HttpClient.Redirect.NEVER).build();
        this.json = json;
    }

    List<String> listModels(String provider, String baseUrl, String apiKey, Duration timeout) {
        HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(baseUrl + (isAnthropic(provider) ? "/v1/models?limit=100" : "/models")))
            .timeout(timeout).GET();
        authorize(builder, provider, apiKey);
        JsonNode body = send(builder.build(), provider);
        List<String> models = new ArrayList<>();
        for (JsonNode item : body.path("data")) {
            String id = item.path("id").asText("");
            if (!id.isBlank()) models.add(id);
        }
        models.sort(String::compareTo);
        return models;
    }

    LLMResponse chat(String provider, String baseUrl, String apiKey, String model, LLMRequest request, int defaultMaxTokens, Duration timeout) {
        Instant started = Instant.now();
        ObjectNode payload = isAnthropic(provider) ? anthropicPayload(model, request, defaultMaxTokens) : openAiPayload(provider, model, request, defaultMaxTokens);
        HttpRequest.Builder builder;
        try {
            builder = HttpRequest.newBuilder(URI.create(baseUrl + (isAnthropic(provider) ? "/v1/messages" : "/chat/completions")))
                .timeout(timeout).header("content-type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(json.writeValueAsString(payload)));
        } catch (IOException exception) {
            throw new ProviderRejectedRequestException("failed to build provider request");
        }
        authorize(builder, provider, apiKey);
        JsonNode body = send(builder.build(), provider);
        long duration = Duration.between(started, Instant.now()).toMillis();

        String text;
        int input;
        int output;
        String toolId = null;
        String toolName = null;
        String toolArgs = null;
        if (isAnthropic(provider)) {
            List<String> parts = new ArrayList<>();
            for (JsonNode block : body.path("content")) {
                String type = block.path("type").asText();
                if ("text".equals(type)) parts.add(block.path("text").asText(""));
                if ("tool_use".equals(type) && toolName == null) {
                    toolId = block.path("id").asText();
                    toolName = block.path("name").asText();
                    toolArgs = block.path("input").isMissingNode() ? "{}" : block.path("input").toString();
                }
            }
            text = String.join("", parts);
            input = body.path("usage").path("input_tokens").asInt(0);
            output = body.path("usage").path("output_tokens").asInt(0);
        } else {
            JsonNode message = body.path("choices").path(0).path("message");
            text = message.path("content").asText("");
            JsonNode call = message.path("tool_calls").path(0);
            if (!call.isMissingNode() && !call.path("function").path("name").asText("").isBlank()) {
                toolId = call.path("id").asText();
                toolName = call.path("function").path("name").asText();
                toolArgs = call.path("function").path("arguments").asText("{}");
            }
            input = body.path("usage").path("prompt_tokens").asInt(0);
            output = body.path("usage").path("completion_tokens").asInt(0);
        }
        String answeredModel = body.path("model").asText(model);
        LLMResponse.Usage usage = new LLMResponse.Usage(input, output, input + output);
        // Fase T: one tool per turn (the agent loop runs them one at a time) — the first one wins.
        String requested = toolName;
        if (requested != null && request.tools().stream().anyMatch(t -> t.name().equals(requested))) {
            return new LLMResponse(UUID.randomUUID(), provider, answeredModel, LLMResponse.KIND_TOOL_USE, null,
                toolId, toolName, toolArgs == null || toolArgs.isBlank() ? "{}" : toolArgs, usage, duration, null, List.of());
        }
        return new LLMResponse(UUID.randomUUID(), provider, answeredModel, LLMResponse.KIND_FINAL, text, null, null, null,
            usage, duration, null, List.of());
    }

    private static final String EMPTY_SCHEMA = "{\"type\":\"object\",\"properties\":{}}";

    private JsonNode schemaOf(LLMRequest.ToolSpec tool) {
        if (tool.inputSchema() != null && tool.inputSchema().isObject()) return tool.inputSchema();
        try {
            return json.readTree(EMPTY_SCHEMA);
        } catch (IOException exception) {
            throw new IllegalStateException(exception);
        }
    }

    private JsonNode argsOf(LLMMessage message) {
        try {
            JsonNode node = json.readTree(message.toolArgsJson() == null || message.toolArgsJson().isBlank() ? "{}" : message.toolArgsJson());
            return node.isObject() ? node : json.createObjectNode();
        } catch (IOException exception) {
            return json.createObjectNode();
        }
    }

    private ObjectNode anthropicPayload(String model, LLMRequest request, int defaultMaxTokens) {
        ObjectNode node = json.createObjectNode();
        node.put("model", model);
        node.put("max_tokens", maxTokens(request, defaultMaxTokens));
        String system = request.messages().stream().filter(m -> m.role().equals("system")).map(LLMMessage::content).collect(Collectors.joining("\n"));
        if (!system.isBlank()) node.put("system", system);
        ArrayNode messages = node.putArray("messages");
        for (LLMMessage m : request.messages()) {
            if (m.role().equals("system")) continue;
            // Anthropic: a tool request is an assistant tool_use block; its result is a user
            // tool_result block. Consecutive messages of the same role are merged (it requires
            // alternating roles).
            String role = m.role().equals("tool") ? "user" : m.role();
            ObjectNode block = json.createObjectNode();
            if (m.isToolRequest()) {
                block.put("type", "tool_use").put("id", m.toolCallId()).put("name", m.toolName()).set("input", argsOf(m));
            } else if (m.role().equals("tool")) {
                block.put("type", "tool_result").put("tool_use_id", m.toolCallId()).put("content", m.content());
            } else {
                block.put("type", "text").put("text", m.content());
            }
            JsonNode last = messages.isEmpty() ? null : messages.get(messages.size() - 1);
            if (last != null && last.path("role").asText().equals(role)) {
                ((ArrayNode) last.path("content")).add(block);
            } else {
                ObjectNode message = messages.addObject().put("role", role);
                message.putArray("content").add(block);
            }
        }
        if (!request.tools().isEmpty()) {
            ArrayNode tools = node.putArray("tools");
            request.tools().forEach(t -> tools.addObject().put("name", t.name()).put("description", t.description()).set("input_schema", schemaOf(t)));
        }
        if (request.parameters() != null && request.parameters().temperature() != null) node.put("temperature", request.parameters().temperature());
        return node;
    }

    private ObjectNode openAiPayload(String provider, String model, LLMRequest request, int defaultMaxTokens) {
        ObjectNode node = json.createObjectNode();
        node.put("model", model);
        // OpenAI's current models reject max_tokens; compatible services (Ollama, Groq…) still use it.
        node.put(ProviderUrlPolicy.OPENAI.equals(provider) ? "max_completion_tokens" : "max_tokens", maxTokens(request, defaultMaxTokens));
        ArrayNode messages = node.putArray("messages");
        for (LLMMessage m : request.messages()) {
            ObjectNode message = messages.addObject().put("role", m.role());
            if (m.isToolRequest()) {
                message.putNull("content");
                ObjectNode call = message.putArray("tool_calls").addObject().put("id", m.toolCallId()).put("type", "function");
                call.putObject("function").put("name", m.toolName()).put("arguments", argsOf(m).toString());
            } else if (m.role().equals("tool")) {
                message.put("tool_call_id", m.toolCallId()).put("content", m.content());
            } else {
                message.put("content", m.content());
            }
        }
        if (!request.tools().isEmpty()) {
            ArrayNode tools = node.putArray("tools");
            request.tools().forEach(t -> {
                ObjectNode fn = tools.addObject().put("type", "function").putObject("function");
                fn.put("name", t.name()).put("description", t.description()).set("parameters", schemaOf(t));
            });
        }
        if (request.parameters() != null && request.parameters().temperature() != null) node.put("temperature", request.parameters().temperature());
        return node;
    }

    private static int maxTokens(LLMRequest request, int fallback) {
        return request.parameters() != null && request.parameters().maxTokens() != null ? request.parameters().maxTokens() : fallback;
    }

    private static boolean isAnthropic(String provider) { return ProviderUrlPolicy.ANTHROPIC.equals(provider); }

    private static void authorize(HttpRequest.Builder builder, String provider, String apiKey) {
        if (isAnthropic(provider)) {
            builder.header("x-api-key", apiKey == null ? "" : apiKey).header("anthropic-version", ANTHROPIC_VERSION);
        } else if (apiKey != null && !apiKey.isBlank()) {
            builder.header("Authorization", "Bearer " + apiKey);
        }
    }

    private JsonNode send(HttpRequest request, String provider) {
        HttpResponse<String> response;
        try {
            response = http.send(request, HttpResponse.BodyHandlers.ofString());
        } catch (HttpTimeoutException exception) {
            throw new ProviderTimeoutException(provider + " request timed out");
        } catch (IOException | InterruptedException exception) {
            if (exception instanceof InterruptedException) Thread.currentThread().interrupt();
            throw new ProviderUnavailableException(provider + " communication failed");
        }
        int status = response.statusCode();
        if (status == 401 || status == 403) throw new ProviderAuthenticationException(provider + " rejected credentials");
        if (status == 429) {
            // OpenAI answers 429 both for "slow down" and for "no credit left"; only the error code
            // tells them apart (read, never echoed).
            if (response.body() != null && response.body().contains("insufficient_quota")) {
                throw new ProviderQuotaExceededException(provider + " account has no credit");
            }
            throw new ProviderRateLimitedException(provider + " rate limited the request");
        }
        if (status >= 500) throw new ProviderUnavailableException(provider + " returned " + status);
        if (status < 200 || status >= 300) throw new ProviderRejectedRequestException(provider + " rejected the request (" + status + ")");
        try {
            return json.readTree(response.body());
        } catch (IOException exception) {
            throw new ProviderUnavailableException(provider + " returned an unparsable response");
        }
    }
}
