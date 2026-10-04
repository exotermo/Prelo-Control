package br.com.exotermo.hermes.app.infrastructure.persistence;
import jakarta.persistence.*; import java.util.UUID;
@Entity @Table(name="task_manual_context_items") public class ManualContextItemEntity {
 @Id public UUID id;
 public UUID taskId;
 public String name;
 public String content;
 @Column(name="item_order") public int itemOrder;
 protected ManualContextItemEntity(){}
}
