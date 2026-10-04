package br.com.exotermo.hermes.app.api;
import br.com.exotermo.hermes.app.application.*; import br.com.exotermo.hermes.app.domain.*; import jakarta.validation.Valid; import jakarta.validation.constraints.NotBlank; import jakarta.validation.constraints.Size; import java.util.List; import java.util.UUID; import org.springframework.http.*; import org.springframework.web.bind.annotation.*; import org.springframework.web.server.ResponseStatusException;
@RestController @RequestMapping("/api/v1/tasks") public class TaskController {
 private final CreateTaskUseCase create; private final ExecuteTaskUseCase execute; private final TaskRepository tasks;
 public TaskController(CreateTaskUseCase create,ExecuteTaskUseCase execute,TaskRepository tasks){this.create=create;this.execute=execute;this.tasks=tasks;}
 @PostMapping public ResponseEntity<TaskResponse> create(@Valid @RequestBody CreateTaskRequest request){
  List<ManualContextItem> items = request.context()==null ? List.of() : request.context().stream().map(c -> new ManualContextItem(c.name(), c.content())).toList();
  requireWithinContextBudget(request.description(), items);
  AgentId agentId = request.agentId()==null || request.agentId().isBlank() ? null : new AgentId(request.agentId());
  Task t=create.create(request.description(), items, agentId);
  return ResponseEntity.status(HttpStatus.CREATED).body(TaskResponse.from(t));
 }
 @PostMapping("/{taskId}/execute") public ExecutionResponse execute(@PathVariable UUID taskId){return ExecutionResponse.from(execute.execute(new TaskId(taskId)));}
 @GetMapping("/{taskId}") public TaskResponse get(@PathVariable UUID taskId){return tasks.findById(new TaskId(taskId)).map(TaskResponse::from).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"task not found"));}

 // Per-field @Size limits (below) bound each value individually, but their sum can still exceed
 // what ContextSnapshot allows for a compiled prompt (MAX_AGGREGATE_CONTENT_LENGTH). Checked
 // explicitly here, before anything is persisted — never rely on the Gateway alone to reject an
 // oversized request that originated from Hermes.
 private void requireWithinContextBudget(String description, List<ManualContextItem> items){
  if (items.size() > ContextSnapshot.MAX_ITEMS) throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "at most " + ContextSnapshot.MAX_ITEMS + " manual context items are allowed");
  long aggregate = description.length() + items.stream().mapToLong(i -> i.content().length()).sum();
  if (aggregate > ContextSnapshot.MAX_AGGREGATE_CONTENT_LENGTH) throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "description plus context content totals " + aggregate + " characters, over the " + ContextSnapshot.MAX_AGGREGATE_CONTENT_LENGTH + " budget");
 }

 // agentId only ever selects an id from the curated catalog (validated in CreateTaskUseCase);
 // directive, model, provider, and capabilities are never accepted from the client.
 public record CreateTaskRequest(@NotBlank @Size(max = Task.MAX_DESCRIPTION_LENGTH) String description, @Size(max = ContextSnapshot.MAX_ITEMS) List<@Valid ManualContextItemRequest> context, String agentId){}
 public record ManualContextItemRequest(@NotBlank @Size(max = ManualContextItem.MAX_NAME_LENGTH) String name, @NotBlank @Size(max = ManualContextItem.MAX_CONTENT_LENGTH) String content){}
 public record TaskResponse(UUID id,String description,TaskStatus status,String agentId){static TaskResponse from(Task t){return new TaskResponse(t.id().value(),t.description(),t.status(),t.agentId().value());}}
 public record ExecutionResponse(UUID executionId,UUID taskId,String agentId,ExecutionStatus status,String result,String error){static ExecutionResponse from(Execution e){return new ExecutionResponse(e.id().value(),e.taskId().value(),e.agentId().value(),e.status(),e.result(),e.error());}}
}
