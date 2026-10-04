package br.com.exotermo.prelo.gateway.provider;

// NOT retryable: the provider rejected the request itself (its own 400-class response) —
// another candidate would receive the same content and likely reject it too.
public class ProviderRejectedRequestException extends RuntimeException {
    public ProviderRejectedRequestException(String message) { super(message); }
    public ProviderRejectedRequestException(String message, Throwable cause) { super(message, cause); }
}
