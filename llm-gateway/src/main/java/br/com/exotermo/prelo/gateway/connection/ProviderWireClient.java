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
 * never echoing upstream bodies or headers. Tool calls are not translated yet — real providers
 * answer FINAL (same limitation as AnthropicProvider, see ADR-014).
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
        ObjectNode payload = isAnthropic(provider) ? anthropicPayload(model, request, defaultMaxTokens) : openAiPayload(model, request, defaultMaxTokens);
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
        if (isAnthropic(provider)) {
            List<String> parts = new ArrayList<>();
            for (JsonNode block : body.path("content")) {
                if ("text".equals(block.path("type").asText())) parts.add(block.path("text").asText(""));
            }
            text = String.join("", parts);
            input = body.path("usage").path("input_tokens").asInt(0);
            output = body.path("usage").path("output_tokens").asInt(0);
        } else {
            text = body.path("choices").path(0).path("message").path("content").asText("");
            input = body.path("usage").path("prompt_tokens").asInt(0);
            output = body.path("usage").path("completion_tokens").asInt(0);
        }
        String answeredModel = body.path("model").asText(model);
        return new LLMResponse(UUID.randomUUID(), provider, answeredModel, LLMResponse.KIND_FINAL, text, null, null, null,
            new LLMResponse.Usage(input, output, input + output), duration, null, List.of());
    }

    private ObjectNode anthropicPayload(String model, LLMRequest request, int defaultMaxTokens) {
        ObjectNode node = json.createObjectNode();
        node.put("model", model);
        node.put("max_tokens", maxTokens(request, defaultMaxTokens));
        String system = request.messages().stream().filter(m -> m.role().equals("system")).map(LLMMessage::content).collect(Collectors.joining("\n"));
        if (!system.isBlank()) node.put("system", system);
        ArrayNode messages = node.putArray("messages");
        request.messages().stream().filter(m -> !m.role().equals("system"))
            .forEach(m -> messages.addObject().put("role", m.role()).put("content", m.content()));
        if (request.parameters() != null && request.parameters().temperature() != null) node.put("temperature", request.parameters().temperature());
        return node;
    }

    private ObjectNode openAiPayload(String model, LLMRequest request, int defaultMaxTokens) {
        ObjectNode node = json.createObjectNode();
        node.put("model", model);
        node.put("max_tokens", maxTokens(request, defaultMaxTokens));
        ArrayNode messages = node.putArray("messages");
        request.messages().forEach(m -> messages.addObject().put("role", m.role()).put("content", m.content()));
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
