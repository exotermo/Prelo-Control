package br.com.exotermo.prelo.gateway.provider;

import static org.junit.jupiter.api.Assertions.assertEquals;
import br.com.exotermo.prelo.gateway.llm.*;
import java.util.List;
import org.junit.jupiter.api.Test;

class MockProviderTest {
    @Test void echoesTheLastUserMessageThroughTheProviderNeutralContract() {
        var response = new MockProvider().execute("mock-echo", new LLMRequest("mock-echo", List.of(new LLMMessage("system", "be concise"), new LLMMessage("user", "hello")), null, null, null));
        assertEquals("mock", response.provider());
        assertEquals(LLMResponse.KIND_FINAL, response.kind());
        assertEquals("[mock] hello", response.content());
    }

    @Test void offersTheFirstToolWhenToolsAreProvidedAndNoResultYet() {
        var request = new LLMRequest("mock-echo", List.of(new LLMMessage("user", "what time is it?")), null, null,
            List.of(new LLMRequest.ToolSpec("current_time", "returns the current time")));
        var response = new MockProvider().execute("mock-echo", request);
        assertEquals(LLMResponse.KIND_TOOL_USE, response.kind());
        assertEquals("current_time", response.toolName());
        assertEquals(null, response.content());
    }

    @Test void answersFinalOnceATooResultIsAlreadyInTheConversation() {
        var request = new LLMRequest("mock-echo", List.of(
            new LLMMessage("user", "what time is it?"),
            new LLMMessage("user", "[tool_result:current_time] 2026-01-01T00:00:00Z")
        ), null, null, List.of(new LLMRequest.ToolSpec("current_time", "returns the current time")));
        var response = new MockProvider().execute("mock-echo", request);
        assertEquals(LLMResponse.KIND_FINAL, response.kind());
    }
}
