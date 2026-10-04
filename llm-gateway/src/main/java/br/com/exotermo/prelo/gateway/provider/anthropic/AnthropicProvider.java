package br.com.exotermo.prelo.gateway.provider.anthropic;

import br.com.exotermo.prelo.gateway.llm.*;
import br.com.exotermo.prelo.gateway.provider.*;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import java.util.stream.Collectors;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.stereotype.Component;

@Component
@ConditionalOnProperty(prefix = "gateway.provider.anthropic", name = "enabled", havingValue = "true")
@EnableConfigurationProperties(AnthropicProviderProperties.class)
public class AnthropicProvider implements LLMProvider {
    private final AnthropicProviderProperties properties;
    private final HttpClient httpClient;
    private final ObjectMapper objectMapper;
    private final String apiKey;

    public AnthropicProvider(AnthropicProviderProperties properties, ProviderSecretLoader secretLoader, ObjectMapper objectMapper) {
        this.properties = properties;
        // Fails application startup if the enabled provider's key file is missing/empty —
        // never deferred to the first request.
        this.apiKey = secretLoader.load(properties.apiKeyFile());
        this.httpClient = HttpClient.newBuilder().connectTimeout(properties.timeout()).build();
        this.objectMapper = objectMapper;
    }

    @Override public String id() { return "anthropic"; }
    @Override public boolean supports(String model) { return properties.models().contains(model); }

    @Override public LLMResponse execute(String model, LLMRequest request) {
        Instant started = Instant.now();
        AnthropicDtos.ChatRequest anthropicRequest = toAnthropicRequest(model, request);
        HttpRequest httpRequest;
        try {
            httpRequest = HttpRequest.newBuilder(URI.create(properties.baseUrl() + "/v1/messages"))
                .header("x-api-key", apiKey)
                .header("anthropic-version", properties.apiVersion())
                .header("content-type", "application/json")
                .timeout(properties.timeout())
                .POST(HttpRequest.BodyPublishers.ofString(objectMapper.writeValueAsString(anthropicRequest)))
                .build();
        } catch (Exception exception) {
            // Never let a body-serialization failure leak request content in its message.
            throw new ProviderRejectedRequestException("failed to build anthropic request");
        }

        HttpResponse<String> httpResponse;
        try {
            httpResponse = httpClient.send(httpRequest, HttpResponse.BodyHandlers.ofString());
        } catch (HttpTimeoutException exception) {
            throw new ProviderTimeoutException("anthropic request timed out");
        } catch (IOException | InterruptedException exception) {
            if (Thread.currentThread().isInterrupted()) Thread.currentThread().interrupt();
            throw new ProviderUnavailableException("anthropic communication failed");
        }

        return switch (httpResponse.statusCode()) {
            case 200 -> toLLMResponse(httpResponse.body(), model, Instant.now().toEpochMilli() - started.toEpochMilli());
            case 401, 403 -> throw new ProviderAuthenticationException("anthropic rejected credentials");
            case 429 -> throw new ProviderRateLimitedException("anthropic rate limited the request");
            default -> {
                if (httpResponse.statusCode() >= 500) throw new ProviderUnavailableException("anthropic returned " + httpResponse.statusCode());
                throw new ProviderRejectedRequestException("anthropic rejected the request (" + httpResponse.statusCode() + ")");
            }
        };
    }

    private AnthropicDtos.ChatRequest toAnthropicRequest(String model, LLMRequest request) {
        String system = request.messages().stream().filter(m -> m.role().equals("system")).map(LLMMessage::content).collect(Collectors.joining("\n"));
        List<AnthropicDtos.Message> messages = request.messages().stream().filter(m -> !m.role().equals("system"))
            .map(m -> new AnthropicDtos.Message(m.role(), m.content())).toList();
        int maxTokens = request.parameters() != null && request.parameters().maxTokens() != null ? request.parameters().maxTokens() : properties.maxTokens();
        Double temperature = request.parameters() != null ? request.parameters().temperature() : null;
        return new AnthropicDtos.ChatRequest(model, maxTokens, system.isBlank() ? null : system, messages, temperature);
    }

    private LLMResponse toLLMResponse(String body, String model, long durationMs) {
        AnthropicDtos.ChatResponse response;
        try {
            response = objectMapper.readValue(body, AnthropicDtos.ChatResponse.class);
        } catch (IOException exception) {
            throw new ProviderUnavailableException("anthropic returned an unparsable response");
        }
        String text = response.content() == null ? "" : response.content().stream()
            .filter(block -> "text".equals(block.type())).map(AnthropicDtos.ContentBlock::text).collect(Collectors.joining());
        int inputTokens = response.usage() != null ? response.usage().inputTokens() : 0;
        int outputTokens = response.usage() != null ? response.usage().outputTokens() : 0;
        // Tool-use passthrough is deliberately not implemented yet (deferred to when the real
        // provider is turned on for the agent loop, per ADR-014) — this provider always answers
        // FINAL today, even if a caller offered tools.
        return new LLMResponse(UUID.randomUUID(), id(), response.model() != null ? response.model() : model, LLMResponse.KIND_FINAL, text,
            null, null, null, new LLMResponse.Usage(inputTokens, outputTokens, inputTokens + outputTokens), durationMs, null, List.of());
    }
}
