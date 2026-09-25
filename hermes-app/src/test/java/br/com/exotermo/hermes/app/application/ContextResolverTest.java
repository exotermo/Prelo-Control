package br.com.exotermo.hermes.app.application;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.*;
import java.util.*;
import org.junit.jupiter.api.Test;

class ContextResolverTest {

    private static ManualContextRepository manualContextWith(TaskId taskId, List<ManualContextItem> items) {
        return new ManualContextRepository() {
            public void save(TaskId t, List<ManualContextItem> i) { throw new UnsupportedOperationException("resolve() must not write manual context"); }
            public List<ManualContextItem> findByTaskId(TaskId t) { return t.equals(taskId) ? items : List.of(); }
        };
    }

    private static ContextSnapshotRepository inMemorySnapshots(Map<ContextSnapshotId, ContextSnapshot> store) {
        return new ContextSnapshotRepository() {
            public ContextSnapshot save(ContextSnapshot s) { store.put(s.id(), s); return s; }
            public Optional<ContextSnapshot> findById(ContextSnapshotId id) { return Optional.ofNullable(store.get(id)); }
            public Optional<ContextSnapshot> findLatestByTaskId(TaskId taskId) {
                return store.values().stream().filter(s -> s.taskId().equals(taskId)).max(Comparator.comparingInt(ContextSnapshot::version));
            }
        };
    }

    @Test void includesOnlyTheTaskDescriptionWhenNoManualItemsExist() {
        Task task = Task.create("summarize this");
        ContextResolver resolver = new ContextResolver(manualContextWith(task.id(), List.of()), inMemorySnapshots(new HashMap<>()));

        ContextSnapshot snapshot = resolver.resolve(task);

        assertEquals(1, snapshot.items().size());
        assertEquals("summarize this", snapshot.items().get(0).content());
        assertEquals(ContextSourceType.MANUAL, snapshot.items().get(0).sourceType());
        assertEquals(task.id(), snapshot.taskId());
    }

    @Test void includesOnlySelectedManualItemsPreservingOriginAndOrder() {
        Task task = Task.create("summarize this");
        List<ManualContextItem> manual = List.of(new ManualContextItem("glossary", "term: definition"), new ManualContextItem("style", "be terse"));
        ContextResolver resolver = new ContextResolver(manualContextWith(task.id(), manual), inMemorySnapshots(new HashMap<>()));

        ContextSnapshot snapshot = resolver.resolve(task);

        assertEquals(3, snapshot.items().size());
        assertEquals("description", snapshot.items().get(0).name());
        assertEquals(0, snapshot.items().get(0).order());
        assertEquals("glossary", snapshot.items().get(1).name());
        assertEquals("term: definition", snapshot.items().get(1).content());
        assertEquals(1, snapshot.items().get(1).order());
        assertEquals("style", snapshot.items().get(2).name());
        assertEquals(2, snapshot.items().get(2).order());
        snapshot.items().forEach(item -> assertEquals(ContextSourceType.MANUAL, item.sourceType()));
    }

    @Test void snapshotItemsAreImmutable() {
        Task task = Task.create("summarize this");
        ContextResolver resolver = new ContextResolver(manualContextWith(task.id(), List.of()), inMemorySnapshots(new HashMap<>()));

        ContextSnapshot snapshot = resolver.resolve(task);

        assertThrows(UnsupportedOperationException.class,
            () -> snapshot.items().add(new ContextSnapshotItem("x", "y", ContextSourceType.MANUAL, "z", 99)));
    }

    @Test void twoResolutionsForTheSameTaskProduceDistinctSnapshotsWithIncreasingVersions() {
        Task task = Task.create("summarize this");
        ContextSnapshotRepository snapshots = inMemorySnapshots(new HashMap<>());
        ContextResolver resolver = new ContextResolver(manualContextWith(task.id(), List.of()), snapshots);

        ContextSnapshot first = snapshots.save(resolver.resolve(task));
        ContextSnapshot second = snapshots.save(resolver.resolve(task));

        assertNotEquals(first.id(), second.id());
        assertEquals(1, first.version());
        assertEquals(2, second.version());
    }
}
