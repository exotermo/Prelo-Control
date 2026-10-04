package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.Execution;
import br.com.exotermo.hermes.app.domain.ExecutionId;
import br.com.exotermo.hermes.app.domain.TaskId;
import java.util.List;
import java.util.Optional;
public interface ExecutionRepository {
 Execution save(Execution execution);
 Optional<Execution> findById(ExecutionId id);
 List<Execution> findByTaskId(TaskId taskId);
}
