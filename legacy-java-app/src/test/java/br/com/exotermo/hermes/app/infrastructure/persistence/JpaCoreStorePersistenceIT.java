package br.com.exotermo.hermes.app.infrastructure.persistence;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.*;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.TestConfiguration;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.context.annotation.Bean;
import org.springframework.orm.ObjectOptimisticLockingFailureException;
import org.springframework.test.context.TestPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Testcontainers;

/**
 * Exercises JpaCoreStore against a real PostgreSQL instance (Testcontainers), the same engine
 * used in production, so Flyway migrations, column types, and optimistic locking are validated
 * for real instead of against an approximate in-memory database or hand-written fakes.
 *
 * Named *IT (not *Test) on purpose: it runs under maven-failsafe-plugin during `mvn verify`,
 * never during `mvn test`/`mvn package` — the Dockerfile's build stage has no Docker socket, so
 * a Testcontainers-based test there would break every image build.
 *
 * How to run: `mvn -pl hermes-app verify` (needs a reachable Docker daemon; if Testcontainers
 * cannot start a container, Spring's context load fails and every test in this class reports
 * that failure loudly — there is no fallback path that would let a missing/broken Docker
 * environment read as a passing test).
 *
 * The Testcontainers-provided database has no `currentSchema` in its JDBC URL (unlike the
 * production URL, which pins `hermes_app`), so we pin Hibernate's default schema explicitly to
 * match where Flyway (spring.flyway.schemas=hermes_app, from application.yml) actually creates
 * the tables — otherwise schema validation looks in `public` and reports every table missing.
 */
@SpringBootTest
@Testcontainers
@TestPropertySource(properties = "spring.jpa.properties.hibernate.default_schema=hermes_app")
class JpaCoreStorePersistenceIT {

    @TestConfiguration
    static class PostgresContainerConfiguration {
        @Bean
        @ServiceConnection
        PostgreSQLContainer<?> postgresContainer() {
            return new PostgreSQLContainer<>("postgres:17-alpine");
        }
    }

    @Autowired private JpaCoreStore store;
    @Autowired private PostgreSQLContainer<?> postgresContainer;

    @Test void theTestcontainersPostgresInstanceIsActuallyRunning() {
        // Explicit guard against a false-green run: if Testcontainers silently fell back to some
        // no-op/mocked connection instead of a real container, this catches it directly.
        assertTrue(postgresContainer.isRunning());
        assertTrue(postgresContainer.isCreated());
    }

    @Test void aTaskSurvivesPersistenceAndReread() {
        Task task = store.save(Task.create("summarize this"));

        Task reloaded = store.findById(task.id()).orElseThrow();

        assertEquals(TaskStatus.CREATED, reloaded.status());
        assertEquals("summarize this", reloaded.description());
        assertEquals(0L, reloaded.version());
    }

    @Test void aStaleTaskWriteIsRejectedByOptimisticLocking() {
        Task task = store.save(Task.create("summarize this"));

        Task firstView = store.findById(task.id()).orElseThrow();
        Task secondView = store.findById(task.id()).orElseThrow();

        store.save(firstView.running());

        assertThrows(ObjectOptimisticLockingFailureException.class,
                () -> store.save(secondView.running()),
                "the second, now-stale view must lose the optimistic-lock race");
    }

    @Test void anExecutionSurvivesTheFullPendingRunningCompletedLifecycle() {
        Task task = store.save(Task.create("summarize this"));

        Execution pending = store.save(Execution.pending(task.id(), new AgentId("general")));
        assertEquals(ExecutionStatus.PENDING, pending.status());
        assertEquals(0L, pending.version());

        Execution running = store.save(store.findById(pending.id()).orElseThrow().running());
        assertEquals(ExecutionStatus.RUNNING, running.status());
        assertEquals(1L, running.version());

        Execution completed = store.save(store.findById(running.id()).orElseThrow()
                .completed("[mock] summarize this", "req-123", "mock-echo", "mock"));
        assertEquals(ExecutionStatus.COMPLETED, completed.status());
        assertEquals(2L, completed.version());

        Execution reloaded = store.findById(completed.id()).orElseThrow();
        assertEquals(ExecutionStatus.COMPLETED, reloaded.status());
        assertEquals("[mock] summarize this", reloaded.result());
        assertEquals("req-123", reloaded.requestId());
        assertEquals("mock-echo", reloaded.model());
        assertEquals("mock", reloaded.provider());
        assertNull(reloaded.error());
        assertEquals(2L, reloaded.version());
    }

    @Test void anExecutionSurvivesTheFullPendingRunningFailedLifecycle() {
        Task task = store.save(Task.create("summarize this"));

        Execution pending = store.save(Execution.pending(task.id(), new AgentId("general")));
        Execution running = store.save(store.findById(pending.id()).orElseThrow().running());
        Execution failed = store.save(store.findById(running.id()).orElseThrow().failed("gateway timeout"));
        assertEquals(2L, failed.version());

        Execution reloaded = store.findById(failed.id()).orElseThrow();
        assertEquals(ExecutionStatus.FAILED, reloaded.status());
        assertEquals("gateway timeout", reloaded.error());
        assertNull(reloaded.result());
        assertEquals(2L, reloaded.version());
    }

    @Test void findByTaskIdReturnsOnlyExecutionsForThatTask() {
        Task taskA = store.save(Task.create("task A"));
        Task taskB = store.save(Task.create("task B"));
        Execution executionA = store.save(Execution.pending(taskA.id(), new AgentId("general")));
        store.save(Execution.pending(taskB.id(), new AgentId("general")));

        var found = store.findByTaskId(taskA.id());

        assertEquals(1, found.size());
        assertEquals(executionA.id(), found.get(0).id());
    }

    @Test void concurrentUpdatesToTheSameExecutionAreRejectedByOptimisticLocking() {
        Task task = store.save(Task.create("summarize this"));
        Execution pending = store.save(Execution.pending(task.id(), new AgentId("general")));

        Execution firstView = store.findById(pending.id()).orElseThrow();
        Execution secondView = store.findById(pending.id()).orElseThrow();

        store.save(firstView.running().completed("first writer wins"));

        assertThrows(ObjectOptimisticLockingFailureException.class,
                () -> store.save(secondView.running().completed("second writer should lose")));
    }
}
