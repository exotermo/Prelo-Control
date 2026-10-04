package dev.prelo.bridge.client;

import dev.prelo.bridge.config.BridgeProperties;
import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClient;

// Talks to prelo-core's authenticated API using a token this bridge mints itself (PreloCoreJwt)
// with the shared PRELO_API_JWT_SECRET: create a task, kick off its async execution (202
// Accepted), then poll GET .../executions/{id} until it reaches a terminal state. Deliberately NOT
// the messaging-core client_credentials token (MessagingCoreClient) — that one's scopes are
// messaging-core's own (messages:send/channels:manage/callbacks:manage) and prelo-core rejects it.
@Component
public class PreloCoreClient {
    private static final Logger log = LoggerFactory.getLogger(PreloCoreClient.class);
    private static final Duration POLL_INTERVAL = Duration.ofMillis(500);
    private static final Duration TOKEN_TTL = Duration.ofMinutes(5);
    private static final List<String> SCOPES = List.of("tasks:create", "tasks:execute", "tasks:read");

    private final RestClient client;
    private final BridgeProperties properties;

    public PreloCoreClient(BridgeProperties properties) {
        this.client = RestClient.builder().baseUrl(properties.preloCoreUrl()).build();
        this.properties = properties;
    }

    private String token() {
        return PreloCoreJwt.mint(properties.preloApiJwtSecret(), properties.preloApiJwtIssuer(),
            properties.preloApiJwtAudience(), SCOPES, TOKEN_TTL);
    }

    /** Outcome of an owner's WhatsApp answer to an approval request (Fase T). */
    public enum DecisionOutcome { APPROVED, DENIED, NOT_FOUND, ALREADY_CLOSED, FAILED }

    // Separate, narrower token: only the owner's answer ever needs approvals:decide-owner.
    private String decisionToken() {
        return PreloCoreJwt.mint(properties.preloApiJwtSecret(), properties.preloApiJwtIssuer(),
            properties.preloApiJwtAudience(), List.of("approvals:decide-owner"), TOKEN_TTL);
    }

    public DecisionOutcome decideApproval(String code, boolean approve, String decidedBy) {
        try {
            client.post().uri("/api/v1/approvals/by-code/{code}/{decision}", code, approve ? "approve" : "deny")
                .header("Authorization", "Bearer " + decisionToken())
                .body(Map.of("decidedBy", decidedBy))
                .retrieve().toBodilessEntity();
            return approve ? DecisionOutcome.APPROVED : DecisionOutcome.DENIED;
        } catch (org.springframework.web.client.HttpClientErrorException exception) {
            if (exception.getStatusCode().value() == 404) return DecisionOutcome.NOT_FOUND;
            if (exception.getStatusCode().value() == 409) return DecisionOutcome.ALREADY_CLOSED;
            log.warn("prelo-core refused an approval decision: {}", exception.getStatusCode());
            return DecisionOutcome.FAILED;
        } catch (RuntimeException exception) {
            log.error("prelo-core: approval decision failed", exception);
            return DecisionOutcome.FAILED;
        }
    }

    public ExecutionResult run(String description, String agentId, String contactAddress, Duration timeout) {
        String taskId;
        try {
            // source=MESSAGING lets prelo-dashboard's Tasks page tell a WhatsApp exchange apart
            // from a task someone actually designated (found 2026-10-02 — every inbound message
            // was showing up indistinguishable from manually created work).
            // contactAddress (Fase C2) is the sender as WhatsApp delivered it (a phone JID or a
            // "@lid" WhatsApp ID): prelo-core ties the task to the client who owns that contact.
            Map<String, Object> body = new java.util.HashMap<>(Map.of("description", description, "agentId", agentId, "source", "MESSAGING"));
            if (contactAddress != null && !contactAddress.isBlank()) {
                body.put("contactAddress", contactAddress);
            }
            Map<?, ?> created = client.post().uri("/api/v1/tasks")
                .header("Authorization", "Bearer " + token())
                .body(body)
                .retrieve().body(Map.class);
            taskId = (String) created.get("id");
        } catch (RuntimeException exception) {
            log.error("prelo-core: failed to create task", exception);
            return new ExecutionResult(null, "ERROR", null, "failed to create task: " + exception.getMessage());
        }

        String executionId;
        try {
            Map<?, ?> execution = client.post().uri("/api/v1/tasks/{taskId}/execute", taskId)
                .header("Authorization", "Bearer " + token())
                .retrieve().body(Map.class);
            executionId = (String) execution.get("executionId");
        } catch (RuntimeException exception) {
            log.error("prelo-core: failed to start execution for task {}", taskId, exception);
            return new ExecutionResult(taskId, "ERROR", null, "failed to start execution: " + exception.getMessage());
        }

        return pollUntilDone(taskId, executionId, timeout);
    }

    private ExecutionResult pollUntilDone(String taskId, String executionId, Duration timeout) {
        Instant deadline = Instant.now().plus(timeout);
        while (Instant.now().isBefore(deadline)) {
            try {
                Map<?, ?> response = client.get().uri("/api/v1/tasks/{taskId}/executions/{executionId}", taskId, executionId)
                    .header("Authorization", "Bearer " + token())
                    .retrieve().body(Map.class);
                String status = (String) response.get("status");
                if ("COMPLETED".equals(status) || "FAILED".equals(status)) {
                    return new ExecutionResult(taskId, status, (String) response.get("result"), (String) response.get("error"));
                }
            } catch (RuntimeException exception) {
                log.error("prelo-core: failed to poll execution {}/{}", taskId, executionId, exception);
                return new ExecutionResult(taskId, "ERROR", null, "failed to poll execution: " + exception.getMessage());
            }
            try {
                Thread.sleep(POLL_INTERVAL.toMillis());
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                return new ExecutionResult(taskId, "ERROR", null, "polling interrupted");
            }
        }
        return new ExecutionResult(taskId, "TIMEOUT", null, "polling timed out after " + timeout);
    }

    public record ExecutionResult(String taskId, String status, String result, String error) { }
}
