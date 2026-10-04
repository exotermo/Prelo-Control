package br.com.exotermo.prelo.gateway.llm;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Positive;
import jakarta.validation.constraints.Size;
import java.util.List;
import java.util.Map;

// modelProfile is a curated name resolved by the Gateway's routing config into an ordered list
// of provider:model candidates (see FallbackPlanResolver) — never a raw provider model chosen by
// the caller. projectId (Fase M, optional) only selects WHICH dashboard-managed connection serves
// the call (the project's own, else the instance default) — the caller still never names a model.
public record LLMRequest(@NotBlank String modelProfile,
                         @NotEmpty @Size(max = 64, message = "messages must contain at most 64 entries") List<@Valid LLMMessage> messages,
                         @Valid Parameters parameters, Map<String, String> metadata,
                         @Size(max = 32, message = "tools must contain at most 32 entries") List<@Valid ToolSpec> tools,
                         @Pattern(regexp = "^[0-9a-fA-F-]{36}$", message = "projectId must be a UUID") String projectId) {
    // Compact constructor: tools defaults to an empty list, never null — every provider can
    // assume request.tools() is safe to iterate/check .isEmpty() on without a null guard.
    public LLMRequest {
        if (tools == null) tools = List.of();
    }

    public LLMRequest(String modelProfile, List<LLMMessage> messages, Parameters parameters, Map<String, String> metadata, List<ToolSpec> tools) {
        this(modelProfile, messages, parameters, metadata, tools, null);
    }

    public java.util.UUID projectUuid() {
        return projectId == null || projectId.isBlank() ? null : java.util.UUID.fromString(projectId);
    }

    public record Parameters(Double temperature, @Positive Integer maxTokens) { }

    // inputSchema (Fase T) is the tool's JSON Schema for its arguments — what real providers need
    // to call it (Anthropic input_schema, OpenAI function.parameters). Absent = no arguments.
    public record ToolSpec(@NotBlank String name, @NotBlank String description, com.fasterxml.jackson.databind.JsonNode inputSchema) {
        public ToolSpec(String name, String description) { this(name, description, null); }
    }
}
