package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.ContextSnapshot;
import br.com.exotermo.hermes.app.domain.ContextSnapshotId;
import br.com.exotermo.hermes.app.domain.TaskId;
import java.util.Optional;
public interface ContextSnapshotRepository {
 ContextSnapshot save(ContextSnapshot snapshot);
 Optional<ContextSnapshot> findById(ContextSnapshotId id);
 Optional<ContextSnapshot> findLatestByTaskId(TaskId taskId);
}
