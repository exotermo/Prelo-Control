package dev.hermes.bridge.client;

import dev.hermes.bridge.config.BridgeProperties;
import java.time.Duration;
import java.time.Instant;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClient;

// Talks to hermes-app-go's authenticated API using the integration token minted by
// messaging-core: create a task, kick off its async execution (202 Accepted), then poll GET
// .../executions/{id} until it reaches a terminal state.
@Component
public class HermesAppClient {
    private static final Logger log = LoggerFactory.getLogger(HermesAppClient.class);
    private static final Duration POLL_INTERVAL = Duration.ofMillis(500);

    private final RestClient client;
    private final MessagingCoreClient messagingCore;

    public HermesAppClient(BridgeProperties properties, MessagingCoreClient messagingCore) {
        this.client = RestClient.builder().baseUrl(properties.hermesAppUrl()).build();
        this.messagingCore = messagingCore;
    }

    public ExecutionResult run(String description, String agentId, Duration timeout) {
        String taskId;
        try {
            Map<?, ?> created = client.post().uri("/api/v1/tasks")
                .header("Authorization", "Bearer " + messagingCore.accessToken())
                .body(Map.of("description", description, "agentId", agentId))
                .retrieve().body(Map.class);
            taskId = (String) created.get("id");
        } catch (RuntimeException exception) {
            log.error("hermes-app: failed to create task", exception);
            return new ExecutionResult(null, "ERROR", null, "failed to create task: " + exception.getMessage());
        }

        String executionId;
        try {
            Map<?, ?> execution = client.post().uri("/api/v1/tasks/{taskId}/execute", taskId)
                .header("Authorization", "Bearer " + messagingCore.accessToken())
                .retrieve().body(Map.class);
            executionId = (String) execution.get("executionId");
        } catch (RuntimeException exception) {
            log.error("hermes-app: failed to start execution for task {}", taskId, exception);
            return new ExecutionResult(taskId, "ERROR", null, "failed to start execution: " + exception.getMessage());
        }

        return pollUntilDone(taskId, executionId, timeout);
    }

    private ExecutionResult pollUntilDone(String taskId, String executionId, Duration timeout) {
        Instant deadline = Instant.now().plus(timeout);
        while (Instant.now().isBefore(deadline)) {
            try {
                Map<?, ?> response = client.get().uri("/api/v1/tasks/{taskId}/executions/{executionId}", taskId, executionId)
                    .header("Authorization", "Bearer " + messagingCore.accessToken())
                    .retrieve().body(Map.class);
                String status = (String) response.get("status");
                if ("COMPLETED".equals(status) || "FAILED".equals(status)) {
                    return new ExecutionResult(taskId, status, (String) response.get("result"), (String) response.get("error"));
                }
            } catch (RuntimeException exception) {
                log.error("hermes-app: failed to poll execution {}/{}", taskId, executionId, exception);
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
