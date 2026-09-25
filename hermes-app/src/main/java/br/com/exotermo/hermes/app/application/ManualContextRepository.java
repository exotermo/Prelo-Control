package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.ManualContextItem;
import br.com.exotermo.hermes.app.domain.TaskId;
import java.util.List;
public interface ManualContextRepository {
 void save(TaskId taskId, List<ManualContextItem> items);
 List<ManualContextItem> findByTaskId(TaskId taskId);
}
