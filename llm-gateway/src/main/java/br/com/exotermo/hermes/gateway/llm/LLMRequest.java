package br.com.exotermo.hermes.gateway.llm;

import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotEmpty;
import jakarta.validation.constraints.Positive;
import java.util.List;
import java.util.Map;

public record LLMRequest(@NotBlank String model, @NotEmpty List<@Valid LLMMessage> messages,
                         @Valid Parameters parameters, Map<String, String> metadata) {
    public record Parameters(Double temperature, @Positive Integer maxTokens) { }
}
