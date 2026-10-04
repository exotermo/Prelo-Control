package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.AgentId;
import br.com.exotermo.hermes.app.domain.ManualContextItem;
import br.com.exotermo.hermes.app.domain.Task;
import java.util.List;
public class CreateTaskUseCase {
 private static final AgentId DEFAULT_AGENT_ID = new AgentId("general");
 private final TaskRepository tasks;
 private final ManualContextRepository manualContext;
 private final AgentDefinitionRegistry agents;
 public CreateTaskUseCase(TaskRepository tasks, ManualContextRepository manualContext, AgentDefinitionRegistry agents) { this.tasks = tasks; this.manualContext = manualContext; this.agents = agents; }
 public Task create(String description) { return create(description, List.of(), null); }
 public Task create(String description, List<ManualContextItem> contextItems) { return create(description, contextItems, null); }
 public Task create(String description, List<ManualContextItem> contextItems, AgentId agentId) {
  AgentId resolvedAgentId = agentId == null ? DEFAULT_AGENT_ID : agentId;
  // Throws UnknownAgentException (mapped to 400) before anything is persisted if the id doesn't
  // resolve in the curated catalog. The client only ever picks an id — directive, model, and
  // capabilities always come from the catalog, never from the request.
  agents.findRequired(resolvedAgentId);
  Task task = tasks.save(Task.create(description, resolvedAgentId));
  if (contextItems != null && !contextItems.isEmpty()) manualContext.save(task.id(), contextItems);
  return task;
 }
}
