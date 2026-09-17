package br.com.exotermo.hermes.gateway.llm;

public interface LLMProvider {
    String id();
    boolean supports(String model);
    LLMResponse execute(LLMRequest request);
}
