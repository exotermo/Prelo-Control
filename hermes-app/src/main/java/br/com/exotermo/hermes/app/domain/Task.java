package br.com.exotermo.hermes.app.domain;
import java.time.Instant;
public record Task(TaskId id, String description, TaskStatus status, Instant createdAt) {
 public Task { if (description == null || description.isBlank()) throw new IllegalArgumentException("task description is required"); }
 public static Task create(String description) { return new Task(TaskId.newId(), description, TaskStatus.CREATED, Instant.now()); }
 public Task running() { return new Task(id, description, TaskStatus.RUNNING, createdAt); }
 public Task completed() { return new Task(id, description, TaskStatus.COMPLETED, createdAt); }
 public Task failed() { return new Task(id, description, TaskStatus.FAILED, createdAt); }
}
