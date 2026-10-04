package br.com.exotermo.hermes.app.application;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.*;
import java.util.List;
import java.util.concurrent.*;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.TestConfiguration;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Primary;
import org.springframework.orm.ObjectOptimisticLockingFailureException;
import org.springframework.test.context.TestPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Testcontainers;

/**
 * Proves that two concurrent attempts to execute the same task cannot both reach the LLM or
 * create an Execution: Task's optimistic locking (added alongside this test) must let exactly
 * one of the two racing writers win, with the other rejected before any Execution is created.
 *
 * How to run: `mvn -pl hermes-app verify` (needs a reachable Docker daemon; see
 * JpaCoreStorePersistenceIT for details on why this is an *IT, not a *Test).
 *
 * The real LlmClient (GatewayLlmClient) is replaced with a fake here: this test is about the
 * Task/Execution concurrency contract, not about actually reaching a running llm-gateway, and
 * the real adapter would just time out trying to connect to a Gateway that doesn't exist in this
 * context.
 */
@SpringBootTest
@Testcontainers
@TestPropertySource(properties = {
    "hermes.gateway.base-url=http://localhost:0",
    "hermes.gateway.jwt-secret=test-secret-at-least-32-bytes-long-000000",
    "hermes.gateway.issuer=test-issuer",
    "hermes.gateway.audience=test-audience",
    "spring.jpa.properties.hibernate.default_schema=hermes_app"
})
class ExecuteTaskUseCaseConcurrencyIT {

    @TestConfiguration
    static class TestBeans {
        @Bean
        @ServiceConnection
        PostgreSQLContainer<?> postgresContainer() {
            return new PostgreSQLContainer<>("postgres:17-alpine");
        }

        @Bean
        @Primary
        LlmClient fakeLlmClient() {
            return (model, messages, requestId, taskId, agentId) -> new LlmClient.ChatResult("[mock] " + messages.get(messages.size() - 1).content(), "mock", model, requestId);
        }
    }

    @Autowired private CreateTaskUseCase createTaskUseCase;
    @Autowired private ExecuteTaskUseCase executeTaskUseCase;
    @Autowired private ExecutionRepository executions;

    @Test void exactlyOneOfTwoConcurrentExecutionAttemptsSucceeds() throws Exception {
        Task task = createTaskUseCase.create("race condition check");
        CyclicBarrier startTogether = new CyclicBarrier(2);
        ExecutorService pool = Executors.newFixedThreadPool(2);
        try {
            List<Future<Execution>> attempts = List.of(
                pool.submit(() -> { startTogether.await(); return executeTaskUseCase.execute(task.id()); }),
                pool.submit(() -> { startTogether.await(); return executeTaskUseCase.execute(task.id()); })
            );

            int succeeded = 0;
            int rejected = 0;
            for (Future<Execution> attempt : attempts) {
                try {
                    attempt.get(10, TimeUnit.SECONDS);
                    succeeded++;
                } catch (ExecutionException e) {
                    Throwable cause = e.getCause();
                    assertTrue(cause instanceof ObjectOptimisticLockingFailureException
                            || cause instanceof InvalidTaskTransitionException,
                        "unexpected failure cause: " + cause);
                    rejected++;
                }
            }

            assertEquals(1, succeeded, "exactly one concurrent attempt should reach the LLM/create an Execution");
            assertEquals(1, rejected, "the other attempt should be rejected by optimistic locking or an invalid transition");
            assertEquals(1, executions.findByTaskId(task.id()).size(), "no duplicate Execution should be created for the same task");
        } finally {
            pool.shutdownNow();
        }
    }
}
