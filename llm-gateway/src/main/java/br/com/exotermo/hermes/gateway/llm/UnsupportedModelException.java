package br.com.exotermo.hermes.gateway.llm;

public class UnsupportedModelException extends RuntimeException {
    public UnsupportedModelException(String profile) { super("No routing configuration for modelProfile: " + profile); }
}
