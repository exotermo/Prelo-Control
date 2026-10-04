package br.com.exotermo.hermes.app.api;
import br.com.exotermo.hermes.app.application.ExecuteTaskUseCase;
import br.com.exotermo.hermes.app.domain.InvalidTaskTransitionException;
import br.com.exotermo.hermes.app.domain.UnknownAgentException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.orm.ObjectOptimisticLockingFailureException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

// Maps domain/persistence failures to stable, non-leaking HTTP error codes. Bean Validation
// failures (@Valid) and ResponseStatusException already get a sane default from Spring, so they
// are not handled again here.
@RestControllerAdvice
public class ApiExceptionHandler {

    @ExceptionHandler(InvalidTaskTransitionException.class)
    public ResponseEntity<ErrorResponse> invalidTransition(InvalidTaskTransitionException exception) {
        return ResponseEntity.status(HttpStatus.CONFLICT)
            .body(new ErrorResponse("invalid_task_transition", "the task is not in a state that allows this operation"));
    }

    @ExceptionHandler(ObjectOptimisticLockingFailureException.class)
    public ResponseEntity<ErrorResponse> concurrentModification(ObjectOptimisticLockingFailureException exception) {
        return ResponseEntity.status(HttpStatus.CONFLICT)
            .body(new ErrorResponse("concurrent_modification", "the resource was modified concurrently; retry with fresh data"));
    }

    @ExceptionHandler(UnknownAgentException.class)
    public ResponseEntity<ErrorResponse> unknownAgent(UnknownAgentException exception) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
            .body(new ErrorResponse("unknown_agent", "agentId does not match any agent in the catalog"));
    }

    @ExceptionHandler(ExecuteTaskUseCase.TaskNotFoundException.class)
    public ResponseEntity<ErrorResponse> taskNotFound(ExecuteTaskUseCase.TaskNotFoundException exception) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
            .body(new ErrorResponse("task_not_found", "task not found"));
    }

    public record ErrorResponse(String code, String message) { }
}
