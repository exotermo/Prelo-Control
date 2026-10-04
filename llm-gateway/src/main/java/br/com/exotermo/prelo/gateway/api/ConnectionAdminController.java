package br.com.exotermo.prelo.gateway.api;

import br.com.exotermo.prelo.gateway.connection.ConnectionService;
import br.com.exotermo.prelo.gateway.connection.ConnectionService.*;
import br.com.exotermo.prelo.gateway.connection.ConnectionValidationException;
import java.util.List;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.web.bind.annotation.*;

/**
 * Fase M admin surface, called only by prelo-core (scope llm:admin, never handed to agents) on
 * behalf of a dashboard ADMIN. Responses never contain a key — only whether one is stored and,
 * for long keys, its last 4 characters.
 */
@RestController
@RequestMapping("/api/v1/admin/connections")
@PreAuthorize("hasAuthority('SCOPE_llm:admin')")
public class ConnectionAdminController {
    private final ConnectionService connections;

    public ConnectionAdminController(ConnectionService connections) { this.connections = connections; }

    public record TestRequest(String provider, String baseUrl, String apiKey) { }
    public record SaveRequest(String provider, String baseUrl, String model, String apiKey) { }
    public record ActiveRequest(boolean active) { }

    @GetMapping("/instance")
    public ConnectionSummary instance() { return connections.summary(null); }

    @PutMapping("/instance")
    public ConnectionSummary saveInstance(@RequestBody SaveRequest body) {
        return connections.save(null, new SaveCommand(body.provider(), body.baseUrl(), body.model(), body.apiKey()));
    }

    @PostMapping("/instance/retest")
    public RetestResult retestInstance() { return connections.retest(null); }

    @DeleteMapping("/instance")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    public void deleteInstance() { connections.delete(null); }

    @GetMapping("/projects/{projectId}")
    public ProjectConnectionView project(@PathVariable UUID projectId) { return connections.projectView(projectId); }

    @PutMapping("/projects/{projectId}")
    public ConnectionSummary saveProject(@PathVariable UUID projectId, @RequestBody SaveRequest body) {
        return connections.save(projectId, new SaveCommand(body.provider(), body.baseUrl(), body.model(), body.apiKey()));
    }

    @PostMapping("/projects/{projectId}/retest")
    public RetestResult retestProject(@PathVariable UUID projectId) { return connections.retest(projectId); }

    @PutMapping("/projects/{projectId}/active")
    public ConnectionSummary setActive(@PathVariable UUID projectId, @RequestBody ActiveRequest body) {
        return connections.setActive(projectId, body.active());
    }

    @DeleteMapping("/projects/{projectId}")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    public void deleteProject(@PathVariable UUID projectId) { connections.delete(projectId); }

    @PostMapping("/test")
    public TestResult test(@RequestBody TestRequest body) { return connections.test(body.provider(), body.baseUrl(), body.apiKey()); }

    @GetMapping("/usage")
    public List<UsageDay> usage(@RequestParam(required = false) UUID projectId, @RequestParam(defaultValue = "7") int days) {
        return connections.usage(projectId, days);
    }

    @ExceptionHandler(ConnectionValidationException.class)
    @ResponseStatus(HttpStatus.BAD_REQUEST)
    public LlmController.ErrorResponse validation(ConnectionValidationException exception) {
        return new LlmController.ErrorResponse("validation_error", exception.getMessage());
    }

    @ExceptionHandler(ConnectionsDisabledException.class)
    @ResponseStatus(HttpStatus.SERVICE_UNAVAILABLE)
    public LlmController.ErrorResponse disabled(ConnectionsDisabledException exception) {
        return new LlmController.ErrorResponse("connections_disabled", "o cofre de conexões não está configurado neste gateway");
    }

    @ExceptionHandler(ConnectionNotFoundException.class)
    @ResponseStatus(HttpStatus.NOT_FOUND)
    public LlmController.ErrorResponse notFound(ConnectionNotFoundException exception) {
        return new LlmController.ErrorResponse("connection_not_found", "nenhuma conexão cadastrada aqui");
    }

    @ExceptionHandler(IllegalStateException.class)
    @ResponseStatus(HttpStatus.INTERNAL_SERVER_ERROR)
    public LlmController.ErrorResponse vaultFailure(IllegalStateException exception) {
        return new LlmController.ErrorResponse("vault_error", "não foi possível abrir a chave guardada");
    }
}
