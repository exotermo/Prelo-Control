package br.com.exotermo.hermes.app.gateway;

import java.util.List;
import java.util.Map;

public final class GatewayContracts {
    private GatewayContracts() { }
    // modelProfile is a curated profile name (e.g. "general-chat"), resolved by the Gateway into
    // an ordered, administered candidate list — never a raw provider model, never a fallback list.
    public record ChatRequest(String modelProfile, List<Message> messages, Parameters parameters, Map<String, String> metadata) { }
    public record Message(String role, String content) { }
    public record Parameters(Double temperature, Integer maxTokens) { }
    public record ChatResponse(String id, String provider, String model, String content, Usage usage, long durationMs, String requestId) { }
    public record Usage(int inputTokens, int outputTokens, int totalTokens) { }
}
