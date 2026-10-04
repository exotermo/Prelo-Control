package br.com.exotermo.hermes.app.application;
import static org.junit.jupiter.api.Assertions.*; import br.com.exotermo.hermes.app.domain.*; import java.util.*; import org.junit.jupiter.api.Test;
class HermesCoreFlowTest {
 private static ExecutionRepository inMemoryExecutions(Map<ExecutionId,Execution> store) {
  return new ExecutionRepository() {
   public Execution save(Execution execution) { store.put(execution.id(), execution); return execution; }
   public Optional<Execution> findById(ExecutionId id) { return Optional.ofNullable(store.get(id)); }
   public List<Execution> findByTaskId(TaskId taskId) { return store.values().stream().filter(e -> e.taskId().equals(taskId)).toList(); }
  };
 }

 private static ManualContextRepository emptyManualContext() {
  return new ManualContextRepository() {
   public void save(TaskId taskId, List<ManualContextItem> items) { }
   public List<ManualContextItem> findByTaskId(TaskId taskId) { return List.of(); }
  };
 }

 private static ContextSnapshotRepository inMemorySnapshots(Map<ContextSnapshotId,ContextSnapshot> store) {
  return new ContextSnapshotRepository() {
   public ContextSnapshot save(ContextSnapshot snapshot) { store.put(snapshot.id(), snapshot); return snapshot; }
   public Optional<ContextSnapshot> findById(ContextSnapshotId id) { return Optional.ofNullable(store.get(id)); }
   public Optional<ContextSnapshot> findLatestByTaskId(TaskId taskId) {
    return store.values().stream().filter(s -> s.taskId().equals(taskId)).max(Comparator.comparingInt(ContextSnapshot::version));
   }
  };
 }

 static AgentDefinitionRegistry generalOnlyRegistry() {
  AgentDefinition general = new AgentDefinition(new AgentId("general"), AgentType.GENERAL, "1", "You are Hermes GeneralAgent. Respond concisely and helpfully.", "mock-echo", List.of(), "General", "test agent");
  return agentId -> agentId.value().equals("general") ? Optional.of(general) : Optional.empty();
 }

 static LlmClient fakeEchoing() {
  return (model, messages, requestId, taskId, agentId) -> new LlmClient.ChatResult("[mock] " + messages.get(1).content(), "mock", model, requestId);
 }

 private static HermesOrchestrator orchestratorWithFake(LlmClient fake) {
  ContextSnapshotRepository snapshots = inMemorySnapshots(new HashMap<>());
  ContextResolver contextResolver = new ContextResolver(emptyManualContext(), snapshots);
  return new HermesOrchestrator(new AgentExecutor(fake), contextResolver, snapshots, generalOnlyRegistry());
 }

 @Test void createsAndExecutesATaskThroughTheGeneralAgent() {
  Map<TaskId,Task> taskData=new HashMap<>(); Map<ExecutionId,Execution> executionData=new HashMap<>();
  TaskRepository tasks=new TaskRepository(){public Task save(Task t){taskData.put(t.id(),t);return t;}public Optional<Task> findById(TaskId id){return Optional.ofNullable(taskData.get(id));}};
  ExecutionRepository executions=inMemoryExecutions(executionData);
  LlmClient fake=fakeEchoing();
  var create=new CreateTaskUseCase(tasks, emptyManualContext(), generalOnlyRegistry()); var execute=new ExecuteTaskUseCase(tasks,executions,orchestratorWithFake(fake));
  Task task=create.create("summarize this"); Execution result=execute.execute(task.id());
  assertEquals(TaskStatus.COMPLETED,tasks.findById(task.id()).orElseThrow().status());assertEquals(ExecutionStatus.COMPLETED,result.status());assertTrue(result.result().startsWith("[mock]"));
  assertNotNull(result.contextSnapshotId());
  assertEquals("general", result.agentId().value());
 }

 @Test void refusesToExecuteATaskThatIsAlreadyFinished() {
  Map<TaskId,Task> taskData=new HashMap<>(); Map<ExecutionId,Execution> executionData=new HashMap<>();
  TaskRepository tasks=new TaskRepository(){public Task save(Task t){taskData.put(t.id(),t);return t;}public Optional<Task> findById(TaskId id){return Optional.ofNullable(taskData.get(id));}};
  ExecutionRepository executions=inMemoryExecutions(executionData);
  LlmClient fake=fakeEchoing();
  var create=new CreateTaskUseCase(tasks, emptyManualContext(), generalOnlyRegistry()); var execute=new ExecuteTaskUseCase(tasks,executions,orchestratorWithFake(fake));
  Task task=create.create("summarize this"); execute.execute(task.id());

  assertThrows(InvalidTaskTransitionException.class, () -> execute.execute(task.id()));
  assertEquals(1, executions.findByTaskId(task.id()).size());
 }
}
