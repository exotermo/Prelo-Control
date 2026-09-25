package br.com.exotermo.hermes.gateway.provider;

import br.com.exotermo.hermes.gateway.llm.*;
import java.util.UUID;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

@Component
@ConditionalOnProperty(prefix = "gateway.provider.mock", name = "enabled", havingValue = "true", matchIfMissing = true)
public class MockProvider implements LLMProvider {
    // Convention (mock-only, see LLMRequest.ToolSpec doc): a message carrying a tool's result is
    // prefixed this way by the caller (hermes-go) when it builds the next turn's messages — lets
    // this deterministic mock tell "haven't asked for a tool yet" apart from "just got one back"
    // without any real provider-side tool_use/tool_result protocol.
    private static final String TOOL_RESULT_PREFIX = "[tool_result:";

    @Override public String id() { return "mock"; }
    @Override public boolean supports(String model) { return model.equals("mock-echo"); }

    @Override public LLMResponse execute(String model, LLMRequest request) {
        java.util.List<LLMMessage> messages = request.messages();
        LLMMessage last = messages.get(messages.size() - 1);
        boolean alreadyHasToolResult = last.content().startsWith(TOOL_RESULT_PREFIX);

        if (!request.tools().isEmpty() && !alreadyHasToolResult) {
            LLMRequest.ToolSpec tool = request.tools().get(0);
            return new LLMResponse(UUID.randomUUID(), id(), model, LLMResponse.KIND_TOOL_USE, null,
                UUID.randomUUID().toString(), tool.name(), "{}",
                new LLMResponse.Usage(0, 0, 0), 0, null, java.util.List.of());
        }

        String lastUserContent = messages.stream().filter(message -> message.role().equals("user")).reduce((first, second) -> second).map(LLMMessage::content).orElse("No user message supplied.");
        String content = "[mock] " + lastUserContent;
        int input = messages.stream().mapToInt(message -> message.content().length() / 4).sum();
        int output = content.length() / 4;
        return new LLMResponse(UUID.randomUUID(), id(), model, LLMResponse.KIND_FINAL, content,
            null, null, null, new LLMResponse.Usage(input, output, input + output), 0, null, java.util.List.of());
    }
}
