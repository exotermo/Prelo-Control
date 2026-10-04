package br.com.exotermo.prelo.gateway.llm;

public interface LLMProvider {
    String id();
    boolean supports(String model);
    // model is the resolved candidate model (from the routing config), never the caller's
    // modelProfile — request carries messages/parameters only, decoupled from any single model.
    LLMResponse execute(String model, LLMRequest request);
}
