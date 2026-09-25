package br.com.exotermo.hermes.app.application;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.*;
import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class AgentExecutorTest {

    private static final AgentDefinition GENERAL = new AgentDefinition(new AgentId("general"), AgentType.GENERAL, "1", "be helpful", "mock-echo", List.of(), "General", "test agent");

    @Test void compilesThePromptOnlyFromTheSnapshotItemsInOrder() {
        List<LlmClient.Message> captured = new ArrayList<>();
        LlmClient capturing = (model, messages, requestId, taskId, agentId) -> { captured.addAll(messages); return new LlmClient.ChatResult("ok", "mock", model, requestId); };

        // Deliberately different from what's in the snapshot: proves AgentExecutor never falls
        // back to task.description() directly, only to what the snapshot actually carries.
        Task task = Task.create("this text must never leak into the prompt directly");
        ContextSnapshot snapshot = ContextSnapshot.resolve(task.id(), 1, List.of(
            new ContextSnapshotItem("description", "summarize the report", ContextSourceType.MANUAL, "task-description", 0),
            new ContextSnapshotItem("style", "be terse", ContextSourceType.MANUAL, "manual-input", 1)
        ));

        new AgentExecutor(capturing).execute(task, GENERAL, snapshot, "req-1");

        assertEquals(2, captured.size());
        assertEquals("system", captured.get(0).role());
        assertEquals("be helpful", captured.get(0).content());
        assertEquals("user", captured.get(1).role());
        assertEquals("description: summarize the report\nstyle: be terse", captured.get(1).content());
    }

    @Test void respectsItemOrderRegardlessOfInputOrdering() {
        List<LlmClient.Message> captured = new ArrayList<>();
        LlmClient capturing = (model, messages, requestId, taskId, agentId) -> { captured.addAll(messages); return new LlmClient.ChatResult("ok", "mock", model, requestId); };

        Task task = Task.create("summarize this");
        // Items handed to the snapshot out of order on purpose.
        ContextSnapshot snapshot = ContextSnapshot.resolve(task.id(), 1, List.of(
            new ContextSnapshotItem("style", "be terse", ContextSourceType.MANUAL, "manual-input", 1),
            new ContextSnapshotItem("description", "summarize the report", ContextSourceType.MANUAL, "task-description", 0)
        ));

        new AgentExecutor(capturing).execute(task, GENERAL, snapshot, "req-1");

        assertEquals("description: summarize the report\nstyle: be terse", captured.get(1).content());
    }

    @Test void sendsTheAgentsModelProfileAndIdentity() {
        List<String> capturedModel = new ArrayList<>();
        List<String> capturedAgentId = new ArrayList<>();
        List<String> capturedRequestId = new ArrayList<>();
        LlmClient capturing = (model, messages, requestId, taskId, agentId) -> {
            capturedModel.add(model); capturedAgentId.add(agentId); capturedRequestId.add(requestId); return new LlmClient.ChatResult("ok", "mock", model, requestId);
        };
        Task task = Task.create("summarize this");
        ContextSnapshot snapshot = ContextSnapshot.resolve(task.id(), 1, List.of(new ContextSnapshotItem("description", "x", ContextSourceType.MANUAL, "task-description", 0)));

        new AgentExecutor(capturing).execute(task, GENERAL, snapshot, "req-42");

        assertEquals(List.of("mock-echo"), capturedModel);
        assertEquals(List.of("general"), capturedAgentId);
        assertEquals(List.of("req-42"), capturedRequestId);
    }
}
