package br.com.exotermo.hermes.app.infrastructure.persistence;
import jakarta.persistence.*; import java.time.Instant; import java.util.UUID;
@Entity @Table(name="tasks") public class TaskEntity {
 @Id public UUID id;
 public String description;
 public String status;
 public Instant createdAt;
 public String agentId;
 @Version public Long taskVersion;
 protected TaskEntity(){}
}
