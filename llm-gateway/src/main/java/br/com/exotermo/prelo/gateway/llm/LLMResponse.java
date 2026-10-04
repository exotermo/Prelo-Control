package br.com.exotermo.prelo.gateway.llm;

import java.util.List;
import java.util.UUID;

// kind is "FINAL" (content carries the answer, tool* fields null) or "TOOL_USE" (content null,
// toolUseId/toolName/toolArgsJson carry what the model wants to call — toolUseId is the
// provider's own identifier for that specific tool-use turn, needed to correlate the result
// sent back on the next turn in whatever shape the real provider eventually requires).
public record LLMResponse(UUID id, String provider, String model, String kind, String content,
                          String toolUseId, String toolName, String toolArgsJson,
                          Usage usage, long durationMs, String requestId, List<AttemptSummary> attempts) {
    public static final String KIND_FINAL = "FINAL";
    public static final String KIND_TOOL_USE = "TOOL_USE";

    public record Usage(int inputTokens, int outputTokens, int totalTokens) { }
    // Resumo de auditoria por tentativa, sem dados sensíveis (sem prompt/resposta/headers).
    public record AttemptSummary(String provider, String model, int order, String outcome) { }
}
