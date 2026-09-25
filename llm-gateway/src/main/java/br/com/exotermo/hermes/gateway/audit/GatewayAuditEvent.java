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
    private String requestedProfile;
    private String provider;
    private String model;
    private Integer attemptOrder;
    private Long durationMs;
    private String detail;
    protected GatewayAuditEvent() { }
    GatewayAuditEvent(String type, String requestId, String subject, String clientId, String requestedProfile, String provider, String model, Integer attemptOrder, Long durationMs, String detail) {
        this.id = UUID.randomUUID(); this.createdAt = Instant.now(); this.eventType = type; this.requestId = requestId; this.subject = subject; this.clientId = clientId;
        this.requestedProfile = requestedProfile; this.provider = provider; this.model = model; this.attemptOrder = attemptOrder; this.durationMs = durationMs; this.detail = detail;
    }
}
