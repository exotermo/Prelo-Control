package br.com.exotermo.hermes.gateway.llm;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;

public record LLMMessage(@Pattern(regexp = "system|user|assistant", message = "role must be system, user, or assistant") String role,
                         @NotBlank String content) { }
