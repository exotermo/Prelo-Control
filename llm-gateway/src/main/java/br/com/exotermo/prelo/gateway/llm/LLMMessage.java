package br.com.exotermo.prelo.gateway.llm;

import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;

/**
 * One conversation message. Fase T adds the tool protocol in a provider-neutral shape: an
 * assistant message that asks for a tool carries toolCallId/toolName/toolArgsJson (content may
 * be empty), and the tool's result comes back as role "tool" with the same toolCallId. Each
 * provider translates this to its own wire format (ProviderWireClient, CliRunnerClient).
 */
public record LLMMessage(@Pattern(regexp = "system|user|assistant|tool", message = "role must be system, user, assistant or tool") String role,
                         @NotNull @Size(max = 32_000, message = "content must be at most 32000 characters") String content,
                         @Size(max = 100) String toolCallId,
                         @Size(max = 64) String toolName,
                         @Size(max = 16_000) String toolArgsJson) {

    public LLMMessage(String role, String content) {
        this(role, content, null, null, null);
    }

    public boolean isToolRequest() { return "assistant".equals(role) && toolName != null && !toolName.isBlank(); }

    @AssertTrue(message = "content is required (except for a tool request), and a tool message needs its toolCallId")
    public boolean isWellFormed() {
        if ("tool".equals(role)) return toolCallId != null && !toolCallId.isBlank();
        if (isToolRequest()) return toolCallId != null && !toolCallId.isBlank();
        return content != null && !content.isBlank();
    }
}
