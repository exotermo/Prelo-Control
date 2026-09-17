package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.Task;
public class CreateTaskUseCase { private final TaskRepository repository; public CreateTaskUseCase(TaskRepository repository) { this.repository = repository; } public Task create(String description) { return repository.save(Task.create(description)); } }
