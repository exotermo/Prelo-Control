package br.com.exotermo.hermes.app.domain;
public record ContextSnapshotItem(String name, String content, ContextSourceType sourceType, String provenance, int order) {
 public ContextSnapshotItem {
  if (name == null || name.isBlank()) throw new IllegalArgumentException("context item name is required");
  if (content == null || content.isBlank()) throw new IllegalArgumentException("context item content is required");
  if (content.length() > ManualContextItem.MAX_CONTENT_LENGTH) throw new IllegalArgumentException("context item content must be at most " + ManualContextItem.MAX_CONTENT_LENGTH + " characters");
 }
}
