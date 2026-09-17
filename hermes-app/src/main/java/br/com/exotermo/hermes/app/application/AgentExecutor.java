package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
public class AgentExecutor {
 private final LlmClient client;
 public AgentExecutor(LlmClient client) { this.client = client; }
 public String execute(Task task, Agent agent, Context context, Directive directive) {
  String contextText = context.items().stream().map(item -> item.name() + ": " + item.content()).reduce("", (a,b) -> a + "\n" + b);
  return client.chat("mock-echo", java.util.List.of(new LlmClient.Message("system", directive.instruction()), new LlmClient.Message("user", task.description() + contextText)), task.id().value().toString(), agent.id().value());
 }
}
