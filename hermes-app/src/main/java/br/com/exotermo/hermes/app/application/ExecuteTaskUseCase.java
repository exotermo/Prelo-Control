package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
public class ExecuteTaskUseCase {
 private final TaskRepository tasks; private final ExecutionRepository executions; private final HermesOrchestrator orchestrator;
 public ExecuteTaskUseCase(TaskRepository tasks, ExecutionRepository executions, HermesOrchestrator orchestrator) { this.tasks=tasks; this.executions=executions; this.orchestrator=orchestrator; }
 public Execution execute(TaskId id) {
  // Capturing the store's return value matters: it carries the row version that the RUNNING
  // write just landed at. Without it, the next save would still carry the pre-write version and
  // spuriously lose the optimistic-lock check against its own prior write. This save is also
  // what makes two concurrent execute() calls on the same task mutually exclusive: only one of
  // them can land this write, the other gets an optimistic-lock failure right here and never
  // reaches agent/context resolution, the LLM, or creates an Execution.
  Task running = tasks.save(tasks.findById(id).orElseThrow(() -> new TaskNotFoundException(id)).running());
  AgentDefinition agent = orchestrator.resolveAgent(running);
  Execution execution = executions.save(Execution.pending(id, running.agentId()).running().withAgentVersion(agent.version()));

  ContextSnapshot snapshot;
  try {
   snapshot = orchestrator.prepareContext(running);
  } catch (RuntimeException error) {
   // Context resolution/persistence itself failed before any snapshot exists — the Execution
   // fails without a contextSnapshotId to point to, since there is genuinely none.
   tasks.save(running.failed());
   return executions.save(execution.failed("context preparation failed: " + error.getMessage()));
  }
  // Persisted immediately, before the LLM call: if the call below fails, the snapshot that was
  // actually used stays associated with the (now FAILED) execution for audit/reproduction.
  execution = executions.save(execution.withContextSnapshotId(snapshot.id()));

  String requestId = execution.id().value().toString();
  try {
   LlmClient.ChatResult result = orchestrator.execute(running, agent, snapshot, requestId);
   tasks.save(running.completed());
   // provider/model here are what the Gateway actually used (post-fallback), never assumed from
   // the requested agent.modelProfile().
   return executions.save(execution.completed(result.content(), result.requestId(), result.model(), result.provider()));
  } catch (RuntimeException error) { tasks.save(running.failed()); return executions.save(execution.withRequestId(requestId).failed(error.getMessage())); }
 }
 public static class TaskNotFoundException extends RuntimeException { public TaskNotFoundException(TaskId id) { super("task not found: " + id.value()); } }
}
