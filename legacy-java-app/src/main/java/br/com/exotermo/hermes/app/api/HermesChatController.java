package br.com.exotermo.hermes.app.api;

import br.com.exotermo.hermes.app.execution.ChatService;
import br.com.exotermo.hermes.app.gateway.GatewayCallException;
import br.com.exotermo.hermes.app.gateway.GatewayContracts.*;
import jakarta.validation.Valid;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotEmpty;
import java.util.List;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1/hermes")
public class HermesChatController {
    private final ChatService service;
    public HermesChatController(ChatService service) { this.service = service; }
    @PostMapping("/chat")
    public ChatResponse chat(@Valid @RequestBody ChatRequest request, @RequestHeader(value = "X-Request-Id", required = false) String suppliedRequestId) {
        String requestId = suppliedRequestId == null ? UUID.randomUUID().toString() : suppliedRequestId;
        return service.chat(new ChatService.ChatCommand(request.model(), request.messages().stream().map(message -> new br.com.exotermo.hermes.app.gateway.GatewayContracts.Message(message.role(), message.content())).toList(), request.parameters() == null ? null : new br.com.exotermo.hermes.app.gateway.GatewayContracts.Parameters(request.parameters().temperature(), request.parameters().maxTokens()), request.taskId(), request.agentId()), requestId);
    }
    @ExceptionHandler(GatewayCallException.class)
    @ResponseStatus(HttpStatus.BAD_GATEWAY)
    public ErrorResponse gatewayFailure(GatewayCallException exception) { return new ErrorResponse("gateway_failure", exception.getMessage()); }

    public record ChatRequest(@NotBlank String model, @NotEmpty List<@Valid Message> messages, @Valid Parameters parameters, String taskId, String agentId) { }
    public record Message(@NotBlank String role, @NotBlank String content) { }
    public record Parameters(Double temperature, Integer maxTokens) { }
    public record ErrorResponse(String code, String message) { }
}
