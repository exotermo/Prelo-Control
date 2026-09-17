package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
public class ExecuteTaskUseCase {
 private final TaskRepository tasks; private final ExecutionRepository executions; private final HermesOrchestrator orchestrator;
 public ExecuteTaskUseCase(TaskRepository tasks, ExecutionRepository executions, HermesOrchestrator orchestrator) { this.tasks=tasks; this.executions=executions; this.orchestrator=orchestrator; }
 public Execution execute(TaskId id) {
  Task running = tasks.findById(id).orElseThrow(() -> new TaskNotFoundException(id)).running(); tasks.save(running); Execution execution = executions.save(Execution.pending(id, Agent.general().id()).running());
  try { String result=orchestrator.execute(running); tasks.save(running.completed()); return executions.save(execution.completed(result)); }
  catch (RuntimeException error) { tasks.save(running.failed()); return executions.save(execution.failed(error.getMessage())); }
 }
 public static class TaskNotFoundException extends RuntimeException { public TaskNotFoundException(TaskId id) { super("task not found: " + id.value()); } }
}
