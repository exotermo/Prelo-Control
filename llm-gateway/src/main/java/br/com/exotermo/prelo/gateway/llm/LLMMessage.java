package br.com.exotermo.prelo.gateway.llm;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;

public record LLMMessage(@Pattern(regexp = "system|user|assistant", message = "role must be system, user, or assistant") String role,
                         @NotBlank @Size(max = 32_000, message = "content must be at most 32000 characters") String content) { }
