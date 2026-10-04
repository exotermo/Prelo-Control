package br.com.exotermo.hermes.app.infrastructure.persistence;
import jakarta.persistence.*; import java.util.UUID;
@Entity @Table(name="context_snapshot_items") public class ContextSnapshotItemEntity {
 @Id public UUID id;
 public UUID snapshotId;
 public String name;
 public String content;
 public String sourceType;
 public String provenance;
 @Column(name="item_order") public int itemOrder;
 protected ContextSnapshotItemEntity(){}
}
