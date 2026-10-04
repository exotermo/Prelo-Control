package br.com.exotermo.prelo.gateway.provider;

// NOT retryable: the Gateway's own credential to the provider is invalid — trying another
// candidate under the same misconfiguration won't help, and masks an ops problem if retried.
public class ProviderAuthenticationException extends RuntimeException {
    public ProviderAuthenticationException(String message) { super(message); }
    public ProviderAuthenticationException(String message, Throwable cause) { super(message, cause); }
}
