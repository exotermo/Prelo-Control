package br.com.exotermo.hermes.app.domain;
import java.time.Instant;
public record Execution(ExecutionId id, TaskId taskId, AgentId agentId, ExecutionStatus status, Instant startedAt, Instant completedAt, String error, String result, String requestId, String model, String provider, String agentVersion, ContextSnapshotId contextSnapshotId, long version) {
 public static Execution pending(TaskId taskId, AgentId agentId) { return new Execution(ExecutionId.newId(), taskId, agentId, ExecutionStatus.PENDING, null, null, null, null, null, null, null, null, null, 0L); }
 public Execution running() { return new Execution(id, taskId, agentId, ExecutionStatus.RUNNING, Instant.now(), null, null, null, requestId, model, provider, agentVersion, contextSnapshotId, version); }
 public Execution completed(String value) { return new Execution(id, taskId, agentId, ExecutionStatus.COMPLETED, startedAt, Instant.now(), null, value, requestId, model, provider, agentVersion, contextSnapshotId, version); }
 // Overload kept for when the Gateway starts returning provider/model effectively used (fase 3).
 public Execution completed(String value, String requestId, String model, String provider) { return new Execution(id, taskId, agentId, ExecutionStatus.COMPLETED, startedAt, Instant.now(), null, value, requestId, model, provider, agentVersion, contextSnapshotId, version); }
 public Execution failed(String message) { return new Execution(id, taskId, agentId, ExecutionStatus.FAILED, startedAt, Instant.now(), message, null, requestId, model, provider, agentVersion, contextSnapshotId, version); }
 // Reflects the row version handed back by the store after a successful write; callers should not construct this by hand.
 public Execution withVersion(long newVersion) { return new Execution(id, taskId, agentId, status, startedAt, completedAt, error, result, requestId, model, provider, agentVersion, contextSnapshotId, newVersion); }
 public Execution withContextSnapshotId(ContextSnapshotId newContextSnapshotId) { return new Execution(id, taskId, agentId, status, startedAt, completedAt, error, result, requestId, model, provider, agentVersion, newContextSnapshotId, version); }
 public Execution withAgentVersion(String newAgentVersion) { return new Execution(id, taskId, agentId, status, startedAt, completedAt, error, result, requestId, model, provider, newAgentVersion, contextSnapshotId, version); }
 public Execution withRequestId(String newRequestId) { return new Execution(id, taskId, agentId, status, startedAt, completedAt, error, result, newRequestId, model, provider, agentVersion, contextSnapshotId, version); }
 public Execution withModel(String newModel) { return new Execution(id, taskId, agentId, status, startedAt, completedAt, error, result, requestId, newModel, provider, agentVersion, contextSnapshotId, version); }
}
