package br.com.exotermo.hermes.app.domain;
// A caller-submitted context item attached to a task at creation time, before it's frozen into
// a ContextSnapshotItem by ContextResolver. Kept distinct from ContextSnapshotItem because it
// carries no order/provenance/source-type yet — those are assigned during resolution.
public record ManualContextItem(String name, String content) {
 // Matches `task_manual_context_items.name VARCHAR(200)` (V5 migration).
 public static final int MAX_NAME_LENGTH = 200;
 // No DB-level cap (content is TEXT), but capped here to protect the compiled prompt budget —
 // see ContextSnapshot.MAX_AGGREGATE_CONTENT_LENGTH. Never rely on the Gateway alone to reject
 // an oversized item coming from Hermes.
 public static final int MAX_CONTENT_LENGTH = 8000;
 public ManualContextItem {
  if (name == null || name.isBlank()) throw new IllegalArgumentException("context item name is required");
  if (name.length() > MAX_NAME_LENGTH) throw new IllegalArgumentException("context item name must be at most " + MAX_NAME_LENGTH + " characters");
  if (content == null || content.isBlank()) throw new IllegalArgumentException("context item content is required");
  if (content.length() > MAX_CONTENT_LENGTH) throw new IllegalArgumentException("context item content must be at most " + MAX_CONTENT_LENGTH + " characters");
 }
}
