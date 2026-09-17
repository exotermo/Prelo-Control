package br.com.exotermo.hermes.gateway.application;

import br.com.exotermo.hermes.gateway.audit.AuditService;
import br.com.exotermo.hermes.gateway.llm.*;
import java.time.Duration;
import java.time.Instant;
import org.springframework.stereotype.Service;

@Service
public class ChatOrchestrationService {
    private final ModelRouter router;
    private final AuditService audit;
    public ChatOrchestrationService(ModelRouter router, AuditService audit) { this.router = router; this.audit = audit; }

    public LLMResponse execute(LLMRequest request, String requestId, String subject, String clientId) {
        Instant started = Instant.now();
        LLMProvider provider = router.route(request.model());
        try {
            LLMResponse response = provider.execute(request);
            long duration = Duration.between(started, Instant.now()).toMillis();
            audit.record("LLM_RESPONSE", requestId, subject, clientId, response.provider(), response.model(), duration, "completed");
            return new LLMResponse(response.id(), response.provider(), response.model(), response.content(), response.usage(), duration);
        } catch (RuntimeException exception) {
            audit.record("PROVIDER_FAILURE", requestId, subject, clientId, provider.id(), request.model(), Duration.between(started, Instant.now()).toMillis(), exception.getClass().getSimpleName());
            throw exception;
        }
    }
}
