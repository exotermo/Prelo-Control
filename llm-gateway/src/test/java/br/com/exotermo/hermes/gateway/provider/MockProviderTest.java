package br.com.exotermo.hermes.gateway.provider;

import static org.junit.jupiter.api.Assertions.assertEquals;
import br.com.exotermo.hermes.gateway.llm.*;
import java.util.List;
import org.junit.jupiter.api.Test;

class MockProviderTest {
    @Test void echoesTheLastUserMessageThroughTheProviderNeutralContract() {
        var response = new MockProvider().execute(new LLMRequest("mock-echo", List.of(new LLMMessage("system", "be concise"), new LLMMessage("user", "hello")), null, null));
        assertEquals("mock", response.provider());
        assertEquals("[mock] hello", response.content());
    }
}
