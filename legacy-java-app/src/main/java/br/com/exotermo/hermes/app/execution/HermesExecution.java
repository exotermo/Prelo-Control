package br.com.exotermo.hermes.app.execution;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "hermes_llm_executions")
public class HermesExecution {
    @Id private UUID id;
    private Instant createdAt;
    private String requestId;
    private String taskId;
    private String agentId;
    private String requestedModel;
    private String provider;
    private Long durationMs;
    private String status;
    protected HermesExecution() { }
    HermesExecution(String requestId, String taskId, String agentId, String requestedModel, String provider, Long durationMs, String status) {
        this.id = UUID.randomUUID(); this.createdAt = Instant.now(); this.requestId = requestId; this.taskId = taskId; this.agentId = agentId; this.requestedModel = requestedModel; this.provider = provider; this.durationMs = durationMs; this.status = status;
    }
}
