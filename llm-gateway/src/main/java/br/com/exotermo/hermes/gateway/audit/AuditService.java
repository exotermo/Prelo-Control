package br.com.exotermo.hermes.gateway.audit;

import org.springframework.stereotype.Service;

@Service
public class AuditService {
    private final GatewayAuditRepository repository;
    AuditService(GatewayAuditRepository repository) { this.repository = repository; }
    public void record(String type, String requestId, String subject, String clientId, String requestedProfile, String provider, String model, Integer attemptOrder, Long durationMs, String detail) {
        repository.save(new GatewayAuditEvent(type, requestId, subject, clientId, requestedProfile, provider, model, attemptOrder, durationMs, detail));
    }
}
