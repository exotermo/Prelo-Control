package br.com.exotermo.hermes.gateway.api;

import br.com.exotermo.hermes.gateway.application.ChatOrchestrationService;
import br.com.exotermo.hermes.gateway.llm.*;
import br.com.exotermo.hermes.gateway.observability.RequestIdPolicy;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.security.oauth2.jwt.Jwt;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1/llm")
public class LlmController {
    private final ChatOrchestrationService chatService;
    public LlmController(ChatOrchestrationService chatService) { this.chatService = chatService; }

    @PostMapping("/chat")
    @PreAuthorize("hasAuthority('SCOPE_llm:invoke')")
    public LLMResponse chat(@Valid @RequestBody LLMRequest request, @AuthenticationPrincipal Jwt jwt,
                            @RequestHeader(value = "X-Request-Id", required = false) String suppliedRequestId) {
        return chatService.execute(request, RequestIdPolicy.resolve(suppliedRequestId), jwt.getSubject(), jwt.getClaimAsString("client_id"));
    }

    @ExceptionHandler(UnsupportedModelException.class)
    @ResponseStatus(HttpStatus.BAD_REQUEST)
    public ErrorResponse unsupportedModel(UnsupportedModelException exception) { return new ErrorResponse("unsupported_model", exception.getMessage()); }
    public record ErrorResponse(String code, String message) { }
}
