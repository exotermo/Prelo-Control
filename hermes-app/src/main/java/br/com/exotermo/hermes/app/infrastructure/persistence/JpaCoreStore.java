package br.com.exotermo.hermes.app.infrastructure.persistence;
import br.com.exotermo.hermes.app.application.*; import br.com.exotermo.hermes.app.domain.*; import java.util.*; import org.springframework.stereotype.Repository;
@Repository public class JpaCoreStore implements TaskRepository, ExecutionRepository {
 private final SpringTaskJpaRepository tasks; private final SpringExecutionJpaRepository executions; JpaCoreStore(SpringTaskJpaRepository tasks, SpringExecutionJpaRepository executions){this.tasks=tasks;this.executions=executions;}
 public Task save(Task t){ TaskEntity e=new TaskEntity();e.id=t.id().value();e.description=t.description();e.status=t.status().name();e.createdAt=t.createdAt();tasks.save(e);return t; }
 public Optional<Task> findById(TaskId id){return tasks.findById(id.value()).map(e->new Task(new TaskId(e.id),e.description,TaskStatus.valueOf(e.status),e.createdAt));}
 public Execution save(Execution x){ExecutionEntity e=new ExecutionEntity();e.id=x.id().value();e.taskId=x.taskId().value();e.agentId=x.agentId().value();e.status=x.status().name();e.startedAt=x.startedAt();e.completedAt=x.completedAt();e.error=x.error();executions.save(e);return x;}
}
