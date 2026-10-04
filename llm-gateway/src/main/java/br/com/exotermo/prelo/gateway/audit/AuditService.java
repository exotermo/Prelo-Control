package br.com.exotermo.prelo.gateway.audit;

import org.springframework.stereotype.Service;

@Service
public class AuditService {
    private final GatewayAuditRepository repository;
    AuditService(GatewayAuditRepository repository) { this.repository = repository; }
    public void record(String type, String requestId, String subject, String clientId, String requestedProfile, String provider, String model, Integer attemptOrder, Long durationMs, String detail) {
        repository.save(new GatewayAuditEvent(type, requestId, subject, clientId, requestedProfile, provider, model, attemptOrder, durationMs, detail));
    }
    // LLM_RESPONSE rows also carry the project and token counts — the dashboard's usage chart reads them.
    public void recordResponse(String requestId, String subject, String clientId, String requestedProfile, String provider, String model,
                               Integer attemptOrder, Long durationMs, java.util.UUID projectId, Integer inputTokens, Integer outputTokens) {
        repository.save(new GatewayAuditEvent("LLM_RESPONSE", requestId, subject, clientId, requestedProfile, provider, model, attemptOrder, durationMs, "completed")
            .withUsage(projectId, inputTokens, outputTokens));
    }
}
