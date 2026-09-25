package br.com.exotermo.hermes.gateway.provider.anthropic;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.util.List;

// Internal wire DTOs for the Anthropic Messages API (https://docs.anthropic.com/en/api/messages).
// Deliberately not shared with the neutral LLMRequest/LLMResponse contract — provider adapters
// convert at the boundary, so Hermes and the rest of the Gateway never see Anthropic's shape.
final class AnthropicDtos {
    private AnthropicDtos() { }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    record ChatRequest(String model, @JsonProperty("max_tokens") int maxTokens, String system, List<Message> messages, Double temperature) { }

    record Message(String role, String content) { }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record ChatResponse(String id, String model, List<ContentBlock> content, Usage usage) { }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record ContentBlock(String type, String text) { }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record Usage(@JsonProperty("input_tokens") int inputTokens, @JsonProperty("output_tokens") int outputTokens) { }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record ErrorEnvelope(ErrorDetail error) { }

    @JsonIgnoreProperties(ignoreUnknown = true)
    record ErrorDetail(String type, String message) { }
}
