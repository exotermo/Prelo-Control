package br.com.exotermo.prelo.gateway.connection;

/** A user-correctable problem with a connection request; its message is safe to show (pt-BR). */
public class ConnectionValidationException extends RuntimeException {
    public ConnectionValidationException(String message) { super(message); }
}
