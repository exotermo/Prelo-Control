package br.com.exotermo.hermes.gateway.provider;

import br.com.exotermo.hermes.gateway.llm.LLMResponse.AttemptSummary;
import java.util.List;

public class FallbackExhaustedException extends RuntimeException {
    private final List<AttemptSummary> attempts;
    private final RuntimeException lastFailure;

    public FallbackExhaustedException(String profile, List<AttemptSummary> attempts, RuntimeException lastFailure) {
        super("all candidates for profile '" + profile + "' failed after " + attempts.size() + " attempt(s)", lastFailure);
        this.attempts = List.copyOf(attempts);
        this.lastFailure = lastFailure;
    }

    public List<AttemptSummary> attempts() { return attempts; }
    public RuntimeException lastFailure() { return lastFailure; }
}
