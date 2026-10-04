package br.com.exotermo.prelo.gateway.connection;

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
import java.util.List;
import java.util.UUID;

/**
 * Fase X: talks to cli-runner, which runs the owner's Claude Code / Codex CLIs (Pro plans) as a
 * text-only model. No provider key ever passes through here — the CLIs hold their own logins.
 */
class CliRunnerClient {
    record EngineStatus(boolean loggedIn, String loginCommand, List<String> models) {
        EngineStatus(boolean loggedIn, String loginCommand) { this(loggedIn, loginCommand, List.of()); }
    }

    private final HttpClient http;
    private final ObjectMapper json;
    private final String baseUrl;
    private final String token;

    CliRunnerClient(ObjectMapper json, String baseUrl, String token) {
        this.http = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(5)).followRedirects(HttpClient.Redirect.NEVER).build();
        this.json = json;
        this.baseUrl = baseUrl;
        this.token = token;
    }

    boolean configured() { return baseUrl != null && !baseUrl.isBlank() && token != null && token.length() >= 32; }

    static String engineOf(String provider) {
        return ProviderUrlPolicy.CLAUDE_CLI.equals(provider) ? "claude" : "codex";
    }

    EngineStatus status(String provider) {
        JsonNode body = send(HttpRequest.newBuilder(URI.create(baseUrl + "/v1/status")).timeout(Duration.ofSeconds(20)).GET(), provider);
        JsonNode engine = body.path(engineOf(provider));
        List<String> models = new java.util.ArrayList<>();
        for (JsonNode m : engine.path("models")) {
            String id = m.path("id").asText("");
            if (!id.isBlank()) models.add(id);
        }
        return new EngineStatus(engine.path("loggedIn").asBoolean(false), engine.path("login").asText(""), models);
    }

    LLMResponse chat(String provider, String model, LLMRequest request, Duration timeout) {
        ObjectNode payload = json.createObjectNode();
        payload.put("engine", engineOf(provider));
        if (model != null && !model.isBlank()) payload.put("model", model);
        payload.put("timeoutMs", timeout.toMillis());
        ArrayNode messages = payload.putArray("messages");
        request.messages().forEach(m -> {
            ObjectNode message = messages.addObject().put("role", m.role()).put("content", m.content());
            if (m.toolCallId() != null) message.put("toolCallId", m.toolCallId());
            if (m.toolName() != null) message.put("toolName", m.toolName());
            if (m.toolArgsJson() != null) message.put("toolArgsJson", m.toolArgsJson());
        });
        if (!request.tools().isEmpty()) {
            ArrayNode tools = payload.putArray("tools");
            request.tools().forEach(t -> {
                ObjectNode tool = tools.addObject().put("name", t.name()).put("description", t.description());
                if (t.inputSchema() != null) tool.set("inputSchema", t.inputSchema());
            });
        }
        HttpRequest.Builder builder;
        try {
            builder = HttpRequest.newBuilder(URI.create(baseUrl + "/v1/run"))
                .timeout(timeout.plusSeconds(15)).header("content-type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(json.writeValueAsString(payload)));
        } catch (IOException exception) {
            throw new ProviderRejectedRequestException("failed to build cli-runner request");
        }
        JsonNode body = send(builder, provider);
        int input = body.path("inputTokens").asInt(0);
        int output = body.path("outputTokens").asInt(0);
        LLMResponse.Usage usage = new LLMResponse.Usage(input, output, input + output);
        String answeredModel = body.path("model").asText(model);
        if ("tool".equals(body.path("kind").asText()) && !body.path("toolName").asText("").isBlank()) {
            // The CLIs have no call ids of their own; the caller's next turn just needs a stable one.
            return new LLMResponse(UUID.randomUUID(), provider, answeredModel, LLMResponse.KIND_TOOL_USE, null,
                "cli_" + UUID.randomUUID().toString().replace("-", ""), body.path("toolName").asText(),
                body.path("toolArgsJson").asText("{}"), usage, body.path("durationMs").asLong(0), null, List.of());
        }
        return new LLMResponse(UUID.randomUUID(), provider, answeredModel, LLMResponse.KIND_FINAL,
            body.path("text").asText(""), null, null, null, usage, body.path("durationMs").asLong(0), null, List.of());
    }

    static final String RUNNER_MESSAGE = "cli-runner: ";

    private String runnerMessage(String body) {
        try {
            String message = json.readTree(body).path("message").asText("");
            return message.isBlank() || message.length() > 280 ? null : RUNNER_MESSAGE + message;
        } catch (IOException | RuntimeException exception) {
            return null;
        }
    }

    private JsonNode send(HttpRequest.Builder builder, String provider) {
        HttpResponse<String> response;
        try {
            response = http.send(builder.header("Authorization", "Bearer " + token).build(), HttpResponse.BodyHandlers.ofString());
        } catch (HttpTimeoutException exception) {
            throw new ProviderTimeoutException(provider + " did not answer in time");
        } catch (IOException | InterruptedException exception) {
            if (exception instanceof InterruptedException) Thread.currentThread().interrupt();
            throw new ProviderUnavailableException("cli-runner is unreachable");
        }
        int status = response.statusCode();
        // The runner's message is ours (Portuguese, no upstream data) — carried with a marker so
        // ConnectionService can show it as-is instead of a generic provider error.
        String reason = runnerMessage(response.body());
        if (status == 402) throw new ProviderQuotaExceededException(reason);
        if (status == 401 && reason != null) throw new ProviderAuthenticationException(reason);
        if (status == 429 && reason != null) throw new ProviderQuotaExceededException(reason);
        if (status >= 500 && reason != null && status != 504) throw new ProviderUnavailableException(reason);
        if (status == 401) {
            // Either the runner token is wrong (ops error) or the CLI itself is logged out.
            throw new ProviderAuthenticationException(provider + " is not logged in");
        }
        if (status == 429) throw new ProviderQuotaExceededException(provider + " plan limit reached");
        if (status == 504) throw new ProviderTimeoutException(provider + " did not answer in time");
        if (status >= 500) throw new ProviderUnavailableException(provider + " failed (" + status + ")");
        if (status < 200 || status >= 300) throw new ProviderRejectedRequestException(provider + " rejected the request (" + status + ")");
        try {
            return json.readTree(response.body());
        } catch (IOException exception) {
            throw new ProviderUnavailableException("cli-runner returned an unparsable response");
        }
    }
}
