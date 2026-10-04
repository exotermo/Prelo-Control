package br.com.exotermo.prelo.gateway.routing;

import static org.junit.jupiter.api.Assertions.*;

import org.junit.jupiter.api.Test;

class ProviderCandidateTest {

    @Test void parsesAWellFormedCandidate() {
        ProviderCandidate candidate = ProviderCandidate.parse("anthropic:claude-3-5-sonnet-latest");
        assertEquals("anthropic", candidate.providerId());
        assertEquals("claude-3-5-sonnet-latest", candidate.model());
    }

    // Regression test: parse() used to throw IllegalArgumentException, a sibling type
    // LlmController's @ExceptionHandler(IllegalStateException.class) ("routing_misconfigured")
    // could never catch, so a malformed config entry propagated uncaught instead of producing
    // the sanitized response the misconfiguration path was designed to produce.
    @Test void malformedCandidateThrowsIllegalStateExceptionNotIllegalArgumentException() {
        assertThrows(IllegalStateException.class, () -> ProviderCandidate.parse("no-colon-here"));
        assertThrows(IllegalStateException.class, () -> ProviderCandidate.parse(":missing-provider"));
        assertThrows(IllegalStateException.class, () -> ProviderCandidate.parse("missing-model:"));
        assertThrows(IllegalStateException.class, () -> ProviderCandidate.parse(null));
    }
}
