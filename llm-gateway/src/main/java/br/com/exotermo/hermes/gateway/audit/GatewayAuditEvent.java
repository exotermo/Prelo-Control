package br.com.exotermo.hermes.gateway.audit;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "gateway_audit_events")
public class GatewayAuditEvent {
    @Id private UUID id;
    private Instant createdAt;
    private String eventType;
    private String requestId;
    private String subject;
    private String clientId;
    private String provider;
    private String model;
    private Long durationMs;
    private String detail;
    protected GatewayAuditEvent() { }
    GatewayAuditEvent(String type, String requestId, String subject, String clientId, String provider, String model, Long durationMs, String detail) {
        this.id = UUID.randomUUID(); this.createdAt = Instant.now(); this.eventType = type; this.requestId = requestId; this.subject = subject; this.clientId = clientId; this.provider = provider; this.model = model; this.durationMs = durationMs; this.detail = detail;
    }
}
