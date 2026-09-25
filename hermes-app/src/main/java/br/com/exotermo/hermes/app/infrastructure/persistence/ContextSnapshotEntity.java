package br.com.exotermo.hermes.app.infrastructure.persistence;
import jakarta.persistence.*; import java.time.Instant; import java.util.UUID;
@Entity @Table(name="context_snapshots") public class ContextSnapshotEntity {
 @Id public UUID id;
 public UUID taskId;
 public int version;
 public Instant resolvedAt;
 protected ContextSnapshotEntity(){}
}
