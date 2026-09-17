package br.com.exotermo.hermes.app.gateway;

import br.com.exotermo.hermes.app.gateway.GatewayContracts.ChatRequest;
import br.com.exotermo.hermes.app.gateway.GatewayContracts.ChatResponse;

public interface LanguageModelGateway {
    ChatResponse chat(ChatRequest request, String requestId);
}
