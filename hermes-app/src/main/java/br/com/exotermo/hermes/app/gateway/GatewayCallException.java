package br.com.exotermo.hermes.app.gateway;

public class GatewayCallException extends RuntimeException {
    private final int status;
    GatewayCallException(int status, String message) { super(message); this.status = status; }
    GatewayCallException(int status, String message, Throwable cause) { super(message, cause); this.status = status; }
    public int status() { return status; }
}
