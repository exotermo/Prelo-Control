package br.com.exotermo.hermes.gateway.provider;

import br.com.exotermo.hermes.gateway.llm.*;
import java.util.UUID;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

@Component
@ConditionalOnProperty(prefix = "gateway.provider.mock", name = "enabled", havingValue = "true", matchIfMissing = true)
public class MockProvider implements LLMProvider {
    @Override public String id() { return "mock"; }
    @Override public boolean supports(String model) { return model.equals("mock-echo"); }
    @Override public LLMResponse execute(LLMRequest request) {
        String lastUserContent = request.messages().stream().filter(message -> message.role().equals("user")).reduce((first, second) -> second).map(LLMMessage::content).orElse("No user message supplied.");
        String content = "[mock] " + lastUserContent;
        int input = request.messages().stream().mapToInt(message -> message.content().length() / 4).sum();
        int output = content.length() / 4;
        return new LLMResponse(UUID.randomUUID(), id(), request.model(), content, new LLMResponse.Usage(input, output, input + output), 0);
    }
}
