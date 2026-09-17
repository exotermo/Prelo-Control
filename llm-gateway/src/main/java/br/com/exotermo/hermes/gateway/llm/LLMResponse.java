package br.com.exotermo.hermes.gateway.llm;

import java.util.UUID;

public record LLMResponse(UUID id, String provider, String model, String content, Usage usage, long durationMs) {
    public record Usage(int inputTokens, int outputTokens, int totalTokens) { }
}
