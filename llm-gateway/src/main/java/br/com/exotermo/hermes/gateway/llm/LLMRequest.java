package br.com.exotermo.hermes.gateway.llm;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.Map;

// modelProfile is a curated name resolved by the Gateway's routing config into an ordered list
// of provider:model candidates (see FallbackPlanResolver) — never a raw provider model chosen by
// the caller.
public record LLMRequest(@NotBlank String modelProfile,
                         @NotEmpty @Size(max = 64, message = "messages must contain at most 64 entries") List<@Valid LLMMessage> messages,
                         @Valid Parameters parameters, Map<String, String> metadata,
                         @Size(max = 32, message = "tools must contain at most 32 entries") List<@Valid ToolSpec> tools) {
    // Compact constructor: tools defaults to an empty list, never null — every provider can
    // assume request.tools() is safe to iterate/check .isEmpty() on without a null guard.
    public LLMRequest {
        if (tools == null) tools = List.of();
    }

    public record Parameters(Double temperature, @Positive Integer maxTokens) { }

    // Deliberately minimal for this slice (name + description only, no JSON schema for args) —
    // enough for MockProvider to offer a tool deterministically. A real provider's tool-use
    // format (e.g. Anthropic's input_schema) is wired when the real provider is turned on, not
    // here.
    public record ToolSpec(@NotBlank String name, @NotBlank String description) { }
}
