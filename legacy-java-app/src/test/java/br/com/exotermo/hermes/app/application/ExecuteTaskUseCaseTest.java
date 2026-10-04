package br.com.exotermo.hermes.app.application;

import static org.junit.jupiter.api.Assertions.*;

import br.com.exotermo.hermes.app.domain.*;
import java.util.*;
import org.junit.jupiter.api.Test;

// Covers the split between context preparation and LLM execution introduced to fix etapa 4:
// a failure after the snapshot was prepared must still leave the snapshot associated with the
// (now FAILED) Execution, for audit/reproduction; a failure *during* preparation must not.
class ExecuteTaskUseCaseTest {

    private static TaskRepository inMemoryTasks(Map<TaskId, Task> store) {
        return new TaskRepository() {
            public Task save(Task t) { store.put(t.id(), t); return t; }
            public Optional<Task> findById(TaskId id) { return Optional.ofNullable(store.get(id)); }
        };
    }

    private static ExecutionRepository inMemoryExecutions(Map<ExecutionId, Execution> store) {
        return new ExecutionRepository() {
            public Execution save(Execution execution) { store.put(execution.id(), execution); return execution; }
            public Optional<Execution> findById(ExecutionId id) { return Optional.ofNullable(store.get(id)); }
            public List<Execution> findByTaskId(TaskId taskId) { return store.values().stream().filter(e -> e.taskId().equals(taskId)).toList(); }
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

    @Test void anLlmFailureAfterContextPreparationKeepsTheSnapshotAssociatedWithTheFailedExecution() {
        Map<TaskId, Task> taskData = new HashMap<>();
        TaskRepository tasks = inMemoryTasks(taskData);
        ExecutionRepository executions = inMemoryExecutions(new HashMap<>());
        ContextSnapshotRepository snapshots = inMemorySnapshots(new HashMap<>());
        ContextResolver resolver = new ContextResolver(emptyManualContext(), snapshots);

        LlmClient failingLlm = (model, messages, requestId, taskId, agentId) -> { throw new RuntimeException("gateway timeout"); };
        HermesOrchestrator orchestrator = new HermesOrchestrator(new AgentExecutor(failingLlm), resolver, snapshots, HermesCoreFlowTest.generalOnlyRegistry());
        ExecuteTaskUseCase execute = new ExecuteTaskUseCase(tasks, executions, orchestrator);

        Task task = tasks.save(Task.create("summarize this"));

        Execution result = execute.execute(task.id());

        assertEquals(ExecutionStatus.FAILED, result.status());
        assertEquals("gateway timeout", result.error());
        assertNotNull(result.contextSnapshotId(), "a snapshot was prepared before the LLM call failed, so it must still be referenced");
        assertTrue(snapshots.findById(result.contextSnapshotId()).isPresent(), "the snapshot itself must remain recoverable");
        assertEquals(TaskStatus.FAILED, tasks.findById(task.id()).orElseThrow().status());
        assertEquals("1", result.agentVersion(), "agentVersion must be recorded even when the execution fails");
    }

    @Test void aSuccessfulExecutionAlsoAssociatesTheSnapshot() {
        TaskRepository tasks = inMemoryTasks(new HashMap<>());
        ExecutionRepository executions = inMemoryExecutions(new HashMap<>());
        ContextSnapshotRepository snapshots = inMemorySnapshots(new HashMap<>());
        ContextResolver resolver = new ContextResolver(emptyManualContext(), snapshots);
        LlmClient fake = (model, messages, requestId, taskId, agentId) -> new LlmClient.ChatResult("[mock] ok", "mock", model, requestId);
        HermesOrchestrator orchestrator = new HermesOrchestrator(new AgentExecutor(fake), resolver, snapshots, HermesCoreFlowTest.generalOnlyRegistry());
        ExecuteTaskUseCase execute = new ExecuteTaskUseCase(tasks, executions, orchestrator);

        Task task = tasks.save(Task.create("summarize this"));
        Execution result = execute.execute(task.id());

        assertEquals(ExecutionStatus.COMPLETED, result.status());
        assertNotNull(result.contextSnapshotId());
        assertTrue(snapshots.findById(result.contextSnapshotId()).isPresent());
        assertNotNull(result.requestId());
        assertEquals("mock-echo", result.model());
        assertEquals("mock", result.provider(), "provider must reflect what the Gateway actually used");
    }

    @Test void aContextPreparationFailureLeavesTheExecutionFailedWithoutASnapshot() {
        TaskRepository tasks = inMemoryTasks(new HashMap<>());
        ExecutionRepository executions = inMemoryExecutions(new HashMap<>());
        ContextSnapshotRepository snapshots = inMemorySnapshots(new HashMap<>());
        ManualContextRepository brokenManualContext = new ManualContextRepository() {
            public void save(TaskId taskId, List<ManualContextItem> items) { }
            public List<ManualContextItem> findByTaskId(TaskId taskId) { throw new RuntimeException("manual context store unavailable"); }
        };
        ContextResolver resolver = new ContextResolver(brokenManualContext, snapshots);
        LlmClient fake = (model, messages, requestId, taskId, agentId) -> new LlmClient.ChatResult("[mock] ok", "mock", model, requestId);
        HermesOrchestrator orchestrator = new HermesOrchestrator(new AgentExecutor(fake), resolver, snapshots, HermesCoreFlowTest.generalOnlyRegistry());
        ExecuteTaskUseCase execute = new ExecuteTaskUseCase(tasks, executions, orchestrator);

        Task task = tasks.save(Task.create("summarize this"));
        Execution result = execute.execute(task.id());

        assertEquals(ExecutionStatus.FAILED, result.status());
        assertNull(result.contextSnapshotId());
        assertEquals("context preparation failed: manual context store unavailable", result.error());
    }

    private static ManualContextRepository emptyManualContext() {
        return new ManualContextRepository() {
            public void save(TaskId taskId, List<ManualContextItem> items) { }
            public List<ManualContextItem> findByTaskId(TaskId taskId) { return List.of(); }
        };
    }
}
