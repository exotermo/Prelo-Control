package br.com.exotermo.hermes.gateway.application;

import br.com.exotermo.hermes.gateway.audit.AuditService;
import br.com.exotermo.hermes.gateway.llm.*;
import br.com.exotermo.hermes.gateway.llm.LLMResponse.AttemptSummary;
import br.com.exotermo.hermes.gateway.provider.*;
import br.com.exotermo.hermes.gateway.routing.FallbackPlanResolver;
import br.com.exotermo.hermes.gateway.routing.ProviderCandidate;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.springframework.stereotype.Service;

// Fallback is a Gateway-owned, configuration-driven decision: Hermes only ever sends a
// modelProfile, never a candidate list, and never chooses (or can influence) the order or
// membership of candidates. An agent cannot pick its own fallback either — it only ever
// requests a modelProfile through AgentDefinition, resolved the same way for everyone.
@Service
public class ChatOrchestrationService {
    private final FallbackPlanResolver plans;
    private final Map<String, LLMProvider> providersById;
    private final AuditService audit;

    public ChatOrchestrationService(FallbackPlanResolver plans, List<LLMProvider> providers, AuditService audit) {
        this.plans = plans;
        this.audit = audit;
        Map<String, LLMProvider> byId = new HashMap<>();
        providers.forEach(provider -> byId.put(provider.id(), provider));
        this.providersById = Map.copyOf(byId);
    }

    public LLMResponse execute(LLMRequest request, String requestId, String subject, String clientId) {
        String profile = request.modelProfile();
        List<ProviderCandidate> candidates = plans.resolve(profile);
        Instant overallStart = Instant.now();
        List<AttemptSummary> attempts = new ArrayList<>();
        RuntimeException lastFailure = null;

        for (int index = 0; index < candidates.size(); index++) {
            int order = index + 1;
            if (Duration.between(overallStart, Instant.now()).compareTo(plans.totalBudget()) >= 0) {
                lastFailure = lastFailure != null ? lastFailure : new ProviderTimeoutException("total request budget exhausted before all candidates were tried");
                break;
            }

            ProviderCandidate candidate = candidates.get(index);
            LLMProvider provider = providersById.get(candidate.providerId());
            if (provider == null || !provider.supports(candidate.model())) {
                // A misconfigured candidate (unknown provider id, or a model the provider itself
                // doesn't recognize) is an ops error, not a transient failure — fail loudly rather
                // than silently skipping it as if it were a normal fallback case.
                throw new IllegalStateException("routing candidate '" + candidate.providerId() + ":" + candidate.model() + "' for profile '" + profile + "' does not resolve to a supporting provider");
            }

            Instant attemptStart = Instant.now();
            try {
                LLMResponse response = provider.execute(candidate.model(), request);
                long attemptDuration = Duration.between(attemptStart, Instant.now()).toMillis();
                attempts.add(new AttemptSummary(candidate.providerId(), candidate.model(), order, "success"));
                audit.record("PROVIDER_ATTEMPT", requestId, subject, clientId, profile, candidate.providerId(), candidate.model(), order, attemptDuration, "success");
                long totalDuration = Duration.between(overallStart, Instant.now()).toMillis();
                audit.record("LLM_RESPONSE", requestId, subject, clientId, profile, response.provider(), response.model(), order, totalDuration, "completed");
                return new LLMResponse(response.id(), response.provider(), response.model(), response.kind(), response.content(),
                    response.toolUseId(), response.toolName(), response.toolArgsJson(), response.usage(), totalDuration, requestId, List.copyOf(attempts));
            } catch (RuntimeException exception) {
                long attemptDuration = Duration.between(attemptStart, Instant.now()).toMillis();
                String outcome = exception.getClass().getSimpleName();
                attempts.add(new AttemptSummary(candidate.providerId(), candidate.model(), order, outcome));
                audit.record("PROVIDER_ATTEMPT", requestId, subject, clientId, profile, candidate.providerId(), candidate.model(), order, attemptDuration, outcome);
                lastFailure = exception;
                if (!retryable(exception)) break;
                // otherwise: fall through to the next candidate
            }
        }

        long totalDuration = Duration.between(overallStart, Instant.now()).toMillis();
        audit.record("PROVIDER_FAILURE", requestId, subject, clientId, profile, null, null, attempts.size(), totalDuration,
            lastFailure == null ? "no candidates configured" : lastFailure.getClass().getSimpleName());
        throw new FallbackExhaustedException(profile, attempts, lastFailure);
    }

    // Only these transitive/backend failures are eligible for fallback. Everything else
    // (validation, malformed request, authentication) fails immediately on the first candidate —
    // trying another candidate would not fix a problem that isn't the candidate's fault.
    private boolean retryable(RuntimeException exception) {
        if (exception instanceof ProviderTimeoutException) return true;
        if (exception instanceof ProviderUnavailableException) return true;
        if (exception instanceof ProviderRateLimitedException) return plans.retryOnRateLimit();
        return false;
    }
}
