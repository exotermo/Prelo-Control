package br.com.exotermo.hermes.gateway.llm;

public class UnsupportedModelException extends RuntimeException {
    public UnsupportedModelException(String model) { super("No enabled provider supports model: " + model); }
}
