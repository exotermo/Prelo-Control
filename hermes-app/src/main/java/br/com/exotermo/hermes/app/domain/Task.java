package br.com.exotermo.hermes.app.domain;
import java.time.Instant;
import java.util.Set;
public record Task(TaskId id, String description, TaskStatus status, Instant createdAt, AgentId agentId, long version) {
 // Matches the `tasks.description VARCHAR(4000)` column (V2 migration): reject at the domain
 // boundary instead of relying on a DB error to catch an oversized value.
 public static final int MAX_DESCRIPTION_LENGTH = 4000;
 private static final Set<TaskStatus> RUNNABLE_FROM = Set.of(TaskStatus.CREATED, TaskStatus.QUEUED);
 private static final Set<TaskStatus> TERMINAL_FROM = Set.of(TaskStatus.RUNNING);
 public Task {
  if (description == null || description.isBlank()) throw new IllegalArgumentException("task description is required");
  if (description.length() > MAX_DESCRIPTION_LENGTH) throw new IllegalArgumentException("task description must be at most " + MAX_DESCRIPTION_LENGTH + " characters");
  if (agentId == null) throw new IllegalArgumentException("task agentId is required");
 }
 public static Task create(String description) { return create(description, new AgentId("general")); }
 public static Task create(String description, AgentId agentId) { return new Task(TaskId.newId(), description, TaskStatus.CREATED, Instant.now(), agentId, 0L); }
 public Task running() { requireStatus(RUNNABLE_FROM, TaskStatus.RUNNING); return new Task(id, description, TaskStatus.RUNNING, createdAt, agentId, version); }
 public Task completed() { requireStatus(TERMINAL_FROM, TaskStatus.COMPLETED); return new Task(id, description, TaskStatus.COMPLETED, createdAt, agentId, version); }
 public Task failed() { requireStatus(TERMINAL_FROM, TaskStatus.FAILED); return new Task(id, description, TaskStatus.FAILED, createdAt, agentId, version); }
 // Reflects the row version handed back by the store after a successful write; callers should not construct this by hand.
 public Task withVersion(long newVersion) { return new Task(id, description, status, createdAt, agentId, newVersion); }
 private void requireStatus(Set<TaskStatus> allowed, TaskStatus target) { if (!allowed.contains(status)) throw new InvalidTaskTransitionException(status, target); }
}
