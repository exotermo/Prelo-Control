package br.com.exotermo.hermes.app.domain;
import java.time.Instant;
public record Execution(ExecutionId id, TaskId taskId, AgentId agentId, ExecutionStatus status, Instant startedAt, Instant completedAt, String error, String result) {
 public static Execution pending(TaskId taskId, AgentId agentId) { return new Execution(ExecutionId.newId(), taskId, agentId, ExecutionStatus.PENDING, null, null, null, null); }
 public Execution running() { return new Execution(id, taskId, agentId, ExecutionStatus.RUNNING, Instant.now(), null, null, null); }
 public Execution completed(String value) { return new Execution(id, taskId, agentId, ExecutionStatus.COMPLETED, startedAt, Instant.now(), null, value); }
 public Execution failed(String message) { return new Execution(id, taskId, agentId, ExecutionStatus.FAILED, startedAt, Instant.now(), message, null); }
}
