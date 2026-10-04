package br.com.exotermo.hermes.app.domain;
import java.time.Instant;
import java.util.List;
public record ContextSnapshot(ContextSnapshotId id, TaskId taskId, int version, Instant resolvedAt, List<ContextSnapshotItem> items) {
 // Curated, explicit budget so a compiled prompt can never silently exceed what the Gateway
 // accepts per message (LLMMessage.content caps at 32000 chars) — checked here, at the domain
 // boundary, not just relied upon from the Gateway's own validation.
 public static final int MAX_ITEMS = 20;
 public static final int MAX_AGGREGATE_CONTENT_LENGTH = 24_000;
 public ContextSnapshot {
  items = List.copyOf(items);
  if (items.isEmpty()) throw new IllegalArgumentException("a context snapshot must have at least one item");
  if (items.size() > MAX_ITEMS) throw new IllegalArgumentException("a context snapshot must have at most " + MAX_ITEMS + " items");
  long aggregateLength = items.stream().mapToLong(item -> item.content().length()).sum();
  if (aggregateLength > MAX_AGGREGATE_CONTENT_LENGTH) throw new IllegalArgumentException("context snapshot content totals " + aggregateLength + " characters, over the " + MAX_AGGREGATE_CONTENT_LENGTH + " budget");
 }
 public static ContextSnapshot resolve(TaskId taskId, int version, List<ContextSnapshotItem> items) {
  return new ContextSnapshot(ContextSnapshotId.newId(), taskId, version, Instant.now(), items);
 }
}
