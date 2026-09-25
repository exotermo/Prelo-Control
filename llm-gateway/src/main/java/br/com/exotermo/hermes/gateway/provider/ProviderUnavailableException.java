package br.com.exotermo.hermes.gateway.provider;

// Retryable: network failure or 5xx from the provider — eligible for fallback.
public class ProviderUnavailableException extends RuntimeException {
    public ProviderUnavailableException(String message) { super(message); }
    public ProviderUnavailableException(String message, Throwable cause) { super(message, cause); }
}
