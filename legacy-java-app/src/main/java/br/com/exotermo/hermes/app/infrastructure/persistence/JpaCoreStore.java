package br.com.exotermo.hermes.app.infrastructure.persistence;
import br.com.exotermo.hermes.app.application.*; import br.com.exotermo.hermes.app.domain.*; import java.util.*; import org.springframework.stereotype.Repository;
@Repository public class JpaCoreStore implements TaskRepository, ExecutionRepository {
 private final SpringTaskJpaRepository tasks; private final SpringExecutionJpaRepository executions; JpaCoreStore(SpringTaskJpaRepository tasks, SpringExecutionJpaRepository executions){this.tasks=tasks;this.executions=executions;}
 public Task save(Task t){
  // Same discipline as Execution below: we never re-read the row's *content* to decide what
  // version to send (that would defeat optimistic locking against a concurrent writer), only
  // whether the row exists yet, since Hibernate treats a non-null @Version as "existing" and
  // Task.create() starts at version 0 (not null).
  boolean exists=tasks.existsById(t.id().value());
  TaskEntity e=new TaskEntity();
  e.id=t.id().value();e.description=t.description();e.status=t.status().name();e.createdAt=t.createdAt();e.agentId=t.agentId().value();e.taskVersion=exists?t.version():null;
  TaskEntity saved=tasks.save(e);return t.withVersion(saved.taskVersion);
 }
 public Optional<Task> findById(TaskId id){return tasks.findById(id.value()).map(e->new Task(new TaskId(e.id),e.description,TaskStatus.valueOf(e.status),e.createdAt,new AgentId(e.agentId),e.taskVersion==null?0L:e.taskVersion));}
 public Execution save(Execution x){
  // Deliberately not re-reading the current row's *content* first: the version we send for an
  // existing row is the one the caller last observed, so a stale write loses the optimistic-lock
  // race instead of silently clobbering a concurrent update. We do need to know whether the row
  // exists yet, though: Hibernate's unsaved-value check treats a non-null @Version as "existing",
  // and Execution.pending() starts at version 0 (not null), so the very first insert must be sent
  // with a null version or Hibernate mistakes it for a lost update on a row that was never there.
  boolean exists=executions.existsById(x.id().value());
  ExecutionEntity e=new ExecutionEntity();
  e.id=x.id().value();e.taskId=x.taskId().value();e.agentId=x.agentId().value();e.status=x.status().name();e.startedAt=x.startedAt();e.completedAt=x.completedAt();
  e.error=x.error();e.result=x.result();e.requestId=x.requestId();e.model=x.model();e.provider=x.provider();e.agentVersion=x.agentVersion();
  e.contextSnapshotId=x.contextSnapshotId()==null?null:x.contextSnapshotId().value();
  e.executionVersion=exists?x.version():null;
  ExecutionEntity saved=executions.save(e);return x.withVersion(saved.executionVersion);
 }
 public Optional<Execution> findById(ExecutionId id){return executions.findById(id.value()).map(this::toDomain);}
 public List<Execution> findByTaskId(TaskId taskId){return executions.findByTaskId(taskId.value()).stream().map(this::toDomain).toList();}
 private Execution toDomain(ExecutionEntity e){return new Execution(new ExecutionId(e.id),new TaskId(e.taskId),new AgentId(e.agentId),ExecutionStatus.valueOf(e.status),e.startedAt,e.completedAt,e.error,e.result,e.requestId,e.model,e.provider,e.agentVersion,e.contextSnapshotId==null?null:new ContextSnapshotId(e.contextSnapshotId),e.executionVersion==null?0L:e.executionVersion);}
}
