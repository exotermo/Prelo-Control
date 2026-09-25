package br.com.exotermo.hermes.app.domain;
import java.util.UUID;
public record ContextSnapshotId(UUID value) { public static ContextSnapshotId newId() { return new ContextSnapshotId(UUID.randomUUID()); } }
