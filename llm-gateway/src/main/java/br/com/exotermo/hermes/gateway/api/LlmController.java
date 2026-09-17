package br.com.exotermo.hermes.gateway.api;

import br.com.exotermo.hermes.gateway.audit.AuditService;
import br.com.exotermo.hermes.gateway.llm.*;
import jakarta.validation.Valid;
import java.time.Duration;
import java.time.Instant;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.security.oauth2.jwt.Jwt;
import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/v1/llm")
public class LlmController {
    private final ModelRouter router;
    private final AuditService audit;
    public LlmController(ModelRouter router, AuditService audit) { this.router = router; this.audit = audit; }

    @PostMapping("/chat")
    @PreAuthorize("hasAuthority('SCOPE_llm:invoke')")
    public LLMResponse chat(@Valid @RequestBody LLMRequest request, @AuthenticationPrincipal Jwt jwt,
                            @RequestHeader(value = "X-Request-Id", required = false) String suppliedRequestId) {
        String requestId = suppliedRequestId == null ? UUID.randomUUID().toString() : suppliedRequestId;
        Instant started = Instant.now();
        LLMProvider provider = router.route(request.model());
        try {
            LLMResponse response = provider.execute(request);
            long duration = Duration.between(started, Instant.now()).toMillis();
            audit.record("LLM_RESPONSE", requestId, jwt.getSubject(), jwt.getClaimAsString("client_id"), response.provider(), response.model(), duration, "completed");
            return new LLMResponse(response.id(), response.provider(), response.model(), response.content(), response.usage(), duration);
        } catch (RuntimeException exception) {
            audit.record("PROVIDER_FAILURE", requestId, jwt.getSubject(), jwt.getClaimAsString("client_id"), provider.id(), request.model(), Duration.between(started, Instant.now()).toMillis(), exception.getClass().getSimpleName());
            throw exception;
        }
    }

    @ExceptionHandler(UnsupportedModelException.class)
    @ResponseStatus(HttpStatus.BAD_REQUEST)
    public ErrorResponse unsupportedModel(UnsupportedModelException exception) { return new ErrorResponse("unsupported_model", exception.getMessage()); }
    public record ErrorResponse(String code, String message) { }
}
