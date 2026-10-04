package br.com.exotermo.prelo.gateway.application;

import br.com.exotermo.prelo.gateway.audit.AuditService;
import br.com.exotermo.prelo.gateway.llm.*;
import br.com.exotermo.prelo.gateway.llm.LLMResponse.AttemptSummary;
import br.com.exotermo.prelo.gateway.provider.*;
import br.com.exotermo.prelo.gateway.routing.FallbackPlanResolver;
import br.com.exotermo.prelo.gateway.routing.ProviderCandidate;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import br.com.exotermo.prelo.gateway.connection.ConnectionRouter;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

// Fallback is a Gateway-owned, configuration-driven decision: Prelo only ever sends a
// modelProfile, never a candidate list, and never chooses (or can influence) the order or
// membership of candidates. An agent cannot pick its own fallback either — it only ever
// requests a modelProfile through AgentDefinition, resolved the same way for everyone.
@Service
public class ChatOrchestrationService {
    private final FallbackPlanResolver plans;
    private final Map<String, LLMProvider> providersById;
    private final AuditService audit;
    private final ConnectionRouter connections;

    public ChatOrchestrationService(FallbackPlanResolver plans, List<LLMProvider> providers, AuditService audit) {
        this(plans, providers, audit, ConnectionRouter.NONE);
    }

    @Autowired
    public ChatOrchestrationService(FallbackPlanResolver plans, List<LLMProvider> providers, AuditService audit, ConnectionRouter connections) {
        this.plans = plans;
        this.audit = audit;
        this.connections = connections;
        Map<String, LLMProvider> byId = new HashMap<>();
        providers.forEach(provider -> byId.put(provider.id(), provider));
        this.providersById = Map.copyOf(byId);
    }

    public LLMResponse execute(LLMRequest request, String requestId, String subject, String clientId) {
        UUID projectId = request.projectUuid();
        Optional<ConnectionRouter.RoutedConnection> routed = connections.route(projectId);
        if (routed.isPresent()) return executeConnection(routed.get(), request, requestId, subject, clientId, projectId);
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
                audit.recordResponse(requestId, subject, clientId, profile, response.provider(), response.model(), order, totalDuration, projectId,
                    response.usage() == null ? null : response.usage().inputTokens(), response.usage() == null ? null : response.usage().outputTokens());
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

    // Fase M: a dashboard-managed connection is a single, explicit choice (the project's own or
    // the instance default) — no silent fallback to another provider; a failure surfaces as-is so
    // the operator sees that *their* connection is broken.
    private LLMResponse executeConnection(ConnectionRouter.RoutedConnection connection, LLMRequest request, String requestId,
                                          String subject, String clientId, UUID projectId) {
        String label = "connection:" + connection.scope().toLowerCase();
        Instant started = Instant.now();
        try {
            LLMResponse response = connection.call().apply(request);
            long duration = Duration.between(started, Instant.now()).toMillis();
            audit.record("PROVIDER_ATTEMPT", requestId, subject, clientId, label, connection.provider(), connection.model(), 1, duration, "success");
            audit.recordResponse(requestId, subject, clientId, label, response.provider(), response.model(), 1, duration, projectId,
                response.usage() == null ? null : response.usage().inputTokens(), response.usage() == null ? null : response.usage().outputTokens());
            return new LLMResponse(response.id(), response.provider(), response.model(), response.kind(), response.content(),
                response.toolUseId(), response.toolName(), response.toolArgsJson(), response.usage(), duration, requestId,
                List.of(new AttemptSummary(connection.provider(), connection.model(), 1, "success")));
        } catch (RuntimeException exception) {
            long duration = Duration.between(started, Instant.now()).toMillis();
            audit.record("PROVIDER_ATTEMPT", requestId, subject, clientId, label, connection.provider(), connection.model(), 1, duration, exception.getClass().getSimpleName());
            throw exception;
        }
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
