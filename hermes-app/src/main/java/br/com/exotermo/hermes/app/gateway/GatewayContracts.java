package br.com.exotermo.hermes.app.gateway;

import java.util.List;
import java.util.Map;

public final class GatewayContracts {
    private GatewayContracts() { }
    public record ChatRequest(String model, List<Message> messages, Parameters parameters, Map<String, String> metadata) { }
    public record Message(String role, String content) { }
    public record Parameters(Double temperature, Integer maxTokens) { }
    public record ChatResponse(String id, String provider, String model, String content, Usage usage, long durationMs) { }
    public record Usage(int inputTokens, int outputTokens, int totalTokens) { }
}
