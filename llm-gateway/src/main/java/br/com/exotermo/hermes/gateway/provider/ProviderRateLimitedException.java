package br.com.exotermo.hermes.gateway.provider;

// Retryable only if gateway.routing.retry-on-rate-limit is true (default true).
public class ProviderRateLimitedException extends RuntimeException {
    public ProviderRateLimitedException(String message) { super(message); }
    public ProviderRateLimitedException(String message, Throwable cause) { super(message, cause); }
}
