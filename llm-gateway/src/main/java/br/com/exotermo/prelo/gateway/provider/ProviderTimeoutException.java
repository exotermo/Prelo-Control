package br.com.exotermo.prelo.gateway.provider;

// Retryable: eligible for fallback to the next candidate.
public class ProviderTimeoutException extends RuntimeException {
    public ProviderTimeoutException(String message) { super(message); }
    public ProviderTimeoutException(String message, Throwable cause) { super(message, cause); }
}
