package br.com.exotermo.hermes.gateway.audit;

import org.springframework.stereotype.Service;

@Service
public class AuditService {
    private final GatewayAuditRepository repository;
    AuditService(GatewayAuditRepository repository) { this.repository = repository; }
    public void record(String type, String requestId, String subject, String clientId, String provider, String model, Long durationMs, String detail) {
        repository.save(new GatewayAuditEvent(type, requestId, subject, clientId, provider, model, durationMs, detail));
    }
}
