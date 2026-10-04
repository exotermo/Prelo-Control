package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
import java.util.Comparator;
import java.util.List;
import java.util.stream.Collectors;
public class AgentExecutor {
 private final LlmClient client;
 public AgentExecutor(LlmClient client) { this.client = client; }
 public LlmClient.ChatResult execute(Task task, AgentDefinition agent, ContextSnapshot snapshot, String requestId) {
  String contextText = snapshot.items().stream()
      .sorted(Comparator.comparingInt(ContextSnapshotItem::order))
      .map(item -> item.name() + ": " + item.content())
      .collect(Collectors.joining("\n"));
  return client.chat(agent.modelProfile(), List.of(new LlmClient.Message("system", agent.directive()), new LlmClient.Message("user", contextText)), requestId, task.id().value().toString(), agent.agentId().value());
 }
}
