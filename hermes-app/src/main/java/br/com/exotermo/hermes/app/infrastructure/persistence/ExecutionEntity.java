package br.com.exotermo.hermes.app.infrastructure.persistence;
import jakarta.persistence.*; import java.time.Instant; import java.util.UUID;
@Entity @Table(name="task_executions") public class ExecutionEntity { @Id public UUID id; public UUID taskId; public String agentId; public String status; public Instant startedAt; public Instant completedAt; public String error; protected ExecutionEntity(){} }
