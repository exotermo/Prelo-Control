package br.com.exotermo.prelo.gateway.routing;

public record ProviderCandidate(String providerId, String model) {
    // IllegalStateException, not IllegalArgumentException: this is thrown from
    // FallbackPlanResolver.resolve(), called from ChatOrchestrationService.execute() with no
    // enclosing try/catch — LlmController only registers an @ExceptionHandler for
    // IllegalStateException ("routing_misconfigured"), so an IllegalArgumentException here
    // (a sibling RuntimeException type) would propagate uncaught straight past it. A malformed
    // 'provider:model' entry in gateway.routing.profiles is exactly the ops-config-typo case
    // that handler exists for.
    public static ProviderCandidate parse(String value) {
        if (value == null) throw new IllegalStateException("candidate must be in 'provider:model' form, got null");
        String[] parts = value.split(":", 2);
        if (parts.length != 2 || parts[0].isBlank() || parts[1].isBlank()) {
            throw new IllegalStateException("candidate must be in 'provider:model' form: " + value);
        }
        return new ProviderCandidate(parts[0], parts[1]);
    }
}
