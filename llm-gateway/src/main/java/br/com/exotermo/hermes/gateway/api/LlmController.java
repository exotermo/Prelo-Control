package br.com.exotermo.hermes.gateway.api;

import br.com.exotermo.hermes.gateway.application.ChatOrchestrationService;
import br.com.exotermo.hermes.gateway.llm.*;
import br.com.exotermo.hermes.gateway.observability.RequestIdPolicy;
import br.com.exotermo.hermes.gateway.provider.*;
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
    public ErrorResponse unsupportedModel(UnsupportedModelException exception) { return new ErrorResponse("unsupported_model_profile", exception.getMessage()); }

    // Immediate (non-retried, single-candidate or first-candidate) provider failures. These map
    // by failure class alone, never leaking upstream response bodies/headers.
    @ExceptionHandler(ProviderTimeoutException.class)
    @ResponseStatus(HttpStatus.GATEWAY_TIMEOUT)
    public ErrorResponse providerTimeout(ProviderTimeoutException exception) { return new ErrorResponse("provider_timeout", "the provider did not respond in time"); }

    @ExceptionHandler(ProviderUnavailableException.class)
    @ResponseStatus(HttpStatus.SERVICE_UNAVAILABLE)
    public ErrorResponse providerUnavailable(ProviderUnavailableException exception) { return new ErrorResponse("provider_unavailable", "the provider is unavailable"); }

    @ExceptionHandler(ProviderRateLimitedException.class)
    @ResponseStatus(HttpStatus.TOO_MANY_REQUESTS)
    public ErrorResponse providerRateLimited(ProviderRateLimitedException exception) { return new ErrorResponse("provider_rate_limited", "the provider rate-limited the request"); }

    @ExceptionHandler(ProviderAuthenticationException.class)
    @ResponseStatus(HttpStatus.BAD_GATEWAY)
    public ErrorResponse providerAuthentication(ProviderAuthenticationException exception) { return new ErrorResponse("provider_authentication_failed", "the gateway's credentials for the provider were rejected"); }

    @ExceptionHandler(ProviderRejectedRequestException.class)
    @ResponseStatus(HttpStatus.BAD_GATEWAY)
    public ErrorResponse providerRejectedRequest(ProviderRejectedRequestException exception) { return new ErrorResponse("provider_rejected_request", "the provider rejected the request"); }

    // All configured candidates were tried (or the total budget ran out) and none succeeded —
    // status is derived from the last failure's class, same mapping as the immediate case above.
    @ExceptionHandler(FallbackExhaustedException.class)
    public org.springframework.http.ResponseEntity<ErrorResponse> fallbackExhausted(FallbackExhaustedException exception) {
        HttpStatus status = switch (exception.lastFailure()) {
            case ProviderTimeoutException ignored -> HttpStatus.GATEWAY_TIMEOUT;
            case ProviderRateLimitedException ignored -> HttpStatus.SERVICE_UNAVAILABLE;
            case ProviderUnavailableException ignored -> HttpStatus.SERVICE_UNAVAILABLE;
            case ProviderAuthenticationException ignored -> HttpStatus.BAD_GATEWAY;
            case ProviderRejectedRequestException ignored -> HttpStatus.BAD_GATEWAY;
            case null, default -> HttpStatus.SERVICE_UNAVAILABLE;
        };
        return org.springframework.http.ResponseEntity.status(status).body(new ErrorResponse("fallback_exhausted", "all configured candidates for the requested profile failed"));
    }

    @ExceptionHandler(IllegalStateException.class)
    @ResponseStatus(HttpStatus.INTERNAL_SERVER_ERROR)
    public ErrorResponse misconfiguration(IllegalStateException exception) { return new ErrorResponse("routing_misconfigured", "the requested profile is misconfigured"); }

    public record ErrorResponse(String code, String message) { }
}
