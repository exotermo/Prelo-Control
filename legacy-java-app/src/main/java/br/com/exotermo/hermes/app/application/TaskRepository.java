package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.Task;
import br.com.exotermo.hermes.app.domain.TaskId;
import java.util.Optional;
public interface TaskRepository { Task save(Task task); Optional<Task> findById(TaskId id); }
