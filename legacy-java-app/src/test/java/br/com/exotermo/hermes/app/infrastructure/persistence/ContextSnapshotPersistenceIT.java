package br.com.exotermo.hermes.app.infrastructure.persistence;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.application.ContextResolver;
import br.com.exotermo.hermes.app.domain.*;
import java.util.List;
import java.util.Optional;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.TestConfiguration;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.context.annotation.Bean;
import org.springframework.test.context.TestPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Testcontainers;

/**
 * How to run: `mvn -pl hermes-app verify` (needs a reachable Docker daemon; see
 * JpaCoreStorePersistenceIT for why this is an *IT, not a *Test, and why the schema property
 * below is required against the Testcontainers-provided database).
 */
@SpringBootTest
@Testcontainers
@TestPropertySource(properties = "spring.jpa.properties.hibernate.default_schema=hermes_app")
class ContextSnapshotPersistenceIT {

    @TestConfiguration
    static class PostgresContainerConfiguration {
        @Bean
        @ServiceConnection
        PostgreSQLContainer<?> postgresContainer() {
            return new PostgreSQLContainer<>("postgres:17-alpine");
        }
    }

    @Autowired private JpaCoreStore taskStore;
    @Autowired private JpaContextSnapshotStore snapshotStore;
    @Autowired private JpaManualContextStore manualContextStore;

    @Test void aSnapshotAndItsItemsSurvivePersistenceAndReread() {
        Task task = taskStore.save(Task.create("summarize this"));
        manualContextStore.save(task.id(), List.of(
            new ManualContextItem("glossary", "term: definition"),
            new ManualContextItem("style", "be terse")));

        ContextSnapshot resolved = new ContextResolver(manualContextStore, snapshotStore).resolve(task);
        ContextSnapshot saved = snapshotStore.save(resolved);

        ContextSnapshot reloaded = snapshotStore.findById(saved.id()).orElseThrow();
        assertEquals(3, reloaded.items().size());
        assertEquals("description", reloaded.items().get(0).name());
        assertEquals("summarize this", reloaded.items().get(0).content());
        assertEquals("glossary", reloaded.items().get(1).name());
        assertEquals("term: definition", reloaded.items().get(1).content());
        assertEquals("style", reloaded.items().get(2).name());
        assertEquals("be terse", reloaded.items().get(2).content());
        assertEquals(0, reloaded.items().get(0).order());
        assertEquals(1, reloaded.items().get(1).order());
        assertEquals(2, reloaded.items().get(2).order());
        reloaded.items().forEach(item -> assertEquals(ContextSourceType.MANUAL, item.sourceType()));

        Optional<ContextSnapshot> latest = snapshotStore.findLatestByTaskId(task.id());
        assertTrue(latest.isPresent());
        assertEquals(saved.id(), latest.get().id());
    }
}
