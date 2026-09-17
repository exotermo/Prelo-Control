package br.com.exotermo.hermes.app.api;
import br.com.exotermo.hermes.app.application.*; import br.com.exotermo.hermes.app.domain.*; import jakarta.validation.constraints.NotBlank; import java.util.UUID; import org.springframework.http.*; import org.springframework.web.bind.annotation.*; import org.springframework.web.server.ResponseStatusException;
@RestController @RequestMapping("/api/v1/tasks") public class TaskController {
 private final CreateTaskUseCase create; private final ExecuteTaskUseCase execute; private final TaskRepository tasks;
 public TaskController(CreateTaskUseCase create,ExecuteTaskUseCase execute,TaskRepository tasks){this.create=create;this.execute=execute;this.tasks=tasks;}
 @PostMapping public ResponseEntity<TaskResponse> create(@RequestBody CreateTaskRequest request){Task t=create.create(request.description());return ResponseEntity.status(HttpStatus.CREATED).body(TaskResponse.from(t));}
 @PostMapping("/{taskId}/execute") public ExecutionResponse execute(@PathVariable UUID taskId){return ExecutionResponse.from(execute.execute(new TaskId(taskId)));}
 @GetMapping("/{taskId}") public TaskResponse get(@PathVariable UUID taskId){return tasks.findById(new TaskId(taskId)).map(TaskResponse::from).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND,"task not found"));}
 public record CreateTaskRequest(@NotBlank String description){} public record TaskResponse(UUID id,String description,TaskStatus status){static TaskResponse from(Task t){return new TaskResponse(t.id().value(),t.description(),t.status());}}
 public record ExecutionResponse(UUID executionId,UUID taskId,String agentId,ExecutionStatus status,String result,String error){static ExecutionResponse from(Execution e){return new ExecutionResponse(e.id().value(),e.taskId().value(),e.agentId().value(),e.status(),e.result(),e.error());}}
}
