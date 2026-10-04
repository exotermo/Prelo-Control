package br.com.exotermo.prelo.gateway.connection;

import br.com.exotermo.prelo.gateway.provider.*;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Instant;
import java.time.LocalDate;
import java.util.Base64;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Fase M: provider connections managed from the dashboard (instance default + per-project
 * override). A connection is only ever persisted after a live test proved its key works — same
 * rule as server registration in prelo-core. The plaintext key exists in memory only while it is
 * being sealed, tested or used for one call; it is never returned, logged or included in errors.
 */
@Service
@EnableConfigurationProperties(ConnectionProperties.class)
public class ConnectionService implements ConnectionRouter {
    private static final Logger log = LoggerFactory.getLogger(ConnectionService.class);
    private static final int MAX_KEY_LENGTH = 512;

    public record ConnectionSummary(boolean configured, String scope, String projectId, String provider, String baseUrl, String model,
                                    boolean active, boolean hasKey, String keyLast4, Instant lastTestAt, Boolean lastTestOk,
                                    Long lastTestLatencyMs, String lastTestError, Instant updatedAt,
                                    Instant lastCallAt, Boolean lastCallOk, String lastCallError) {
        static ConnectionSummary empty(String scope, UUID projectId) {
            return new ConnectionSummary(false, scope, projectId == null ? null : projectId.toString(), null, null, null,
                false, false, null, null, null, null, null, null, null, null, null);
        }

        static ConnectionSummary of(ProviderConnection c) {
            return new ConnectionSummary(true, c.scope(), c.projectId() == null ? null : c.projectId().toString(), c.provider(), c.baseUrl(),
                c.model(), c.active(), c.hasKey(), c.keyLast4(), c.lastTestAt(), c.lastTestOk(), c.lastTestLatencyMs(), c.lastTestError(), c.updatedAt(),
                c.lastCallAt(), c.lastCallOk(), c.lastCallError());
        }
    }

    /** Fase X: whether each subscription CLI is logged in inside cli-runner, and how to log it in. */
    public record CliStatus(boolean available, boolean claudeLoggedIn, String claudeLogin, boolean codexLoggedIn, String codexLogin) { }

    public record ProjectConnectionView(ConnectionSummary own, ConnectionSummary instance, boolean usingOwn) { }
    public record TestResult(boolean ok, long latencyMs, List<String> models, String error) { }
    public record SaveCommand(String provider, String baseUrl, String model, String apiKey) { }
    public record RetestResult(ConnectionSummary connection, TestResult test) { }
    public record UsageDay(LocalDate day, long calls, long inputTokens, long outputTokens) { }

    public static class ConnectionsDisabledException extends RuntimeException {
        public ConnectionsDisabledException() { super("provider connections are not configured on this gateway"); }
    }

    public static class ConnectionNotFoundException extends RuntimeException {
        public ConnectionNotFoundException() { super("connection not found"); }
    }

    private final ProviderConnectionRepository repository;
    private final ConnectionProperties properties;
    private final ProviderWireClient wire;
    private final JdbcTemplate jdbc;
    private final ConnectionKeyCipher cipher;
    private final CliRunnerClient cli;

    @Autowired
    public ConnectionService(ProviderConnectionRepository repository, ConnectionProperties properties, ObjectMapper objectMapper, JdbcTemplate jdbc) {
        this(repository, properties, new ProviderWireClient(objectMapper), jdbc, loadCipher(properties.masterKeyFile()),
            new CliRunnerClient(objectMapper, properties.cliRunnerUrl(), properties.cliRunnerToken()));
    }

    ConnectionService(ProviderConnectionRepository repository, ConnectionProperties properties, ProviderWireClient wire, JdbcTemplate jdbc, ConnectionKeyCipher cipher) {
        this(repository, properties, wire, jdbc, cipher, null);
    }

    ConnectionService(ProviderConnectionRepository repository, ConnectionProperties properties, ProviderWireClient wire, JdbcTemplate jdbc,
                      ConnectionKeyCipher cipher, CliRunnerClient cli) {
        this.repository = repository;
        this.properties = properties;
        this.wire = wire;
        this.jdbc = jdbc;
        this.cipher = cipher;
        this.cli = cli;
    }

    private static ConnectionKeyCipher loadCipher(String masterKeyFile) {
        Path path = Path.of(masterKeyFile);
        if (!Files.isReadable(path)) {
            log.warn("gateway connections disabled: master key file not present (dashboard-managed model connections unavailable)");
            return null;
        }
        try {
            String encoded = Files.readString(path).strip();
            if (encoded.isEmpty()) {
                log.warn("gateway connections disabled: master key file is empty");
                return null;
            }
            return new ConnectionKeyCipher(Base64.getDecoder().decode(encoded));
        } catch (IOException | IllegalArgumentException exception) {
            // A present-but-broken key is an ops error: fail startup instead of silently disabling.
            throw new IllegalStateException("gateway connections master key is invalid (expected 32 bytes, Base64)");
        }
    }

    public boolean enabled() { return cipher != null; }

    private void requireEnabled() {
        if (cipher == null) throw new ConnectionsDisabledException();
    }

    private Optional<ProviderConnection> find(UUID projectId) {
        return projectId == null
            ? repository.findFirstByScope(ProviderConnection.SCOPE_INSTANCE)
            : repository.findByScopeAndProjectId(ProviderConnection.SCOPE_PROJECT, projectId);
    }

    private static String scopeOf(UUID projectId) {
        return projectId == null ? ProviderConnection.SCOPE_INSTANCE : ProviderConnection.SCOPE_PROJECT;
    }

    // --- reads ---

    @Transactional(readOnly = true)
    public ConnectionSummary summary(UUID projectId) {
        requireEnabled();
        return find(projectId).map(ConnectionSummary::of).orElse(ConnectionSummary.empty(scopeOf(projectId), projectId));
    }

    @Transactional(readOnly = true)
    public ProjectConnectionView projectView(UUID projectId) {
        ConnectionSummary own = summary(projectId);
        ConnectionSummary instance = summary(null);
        return new ProjectConnectionView(own, instance, own.configured() && own.active());
    }

    // --- test without saving ---

    public TestResult test(String provider, String baseUrl, String apiKey) {
        requireEnabled();
        if (!ProviderUrlPolicy.knownProvider(provider)) throw new ConnectionValidationException("provedor desconhecido");
        String resolvedUrl = ProviderUrlPolicy.resolveBaseUrl(provider, baseUrl, properties.allowPrivateUrls());
        String key = blankToNull(apiKey);
        if (ProviderUrlPolicy.requiresKey(provider) && key == null) throw new ConnectionValidationException("informe a chave de API");
        return probe(provider, resolvedUrl, key, null);
    }

    private TestResult probe(String provider, String baseUrl, String key, String model) {
        if (ProviderUrlPolicy.isCli(provider)) return probeCli(provider, model);
        long started = System.nanoTime();
        try {
            List<String> models = wire.listModels(provider, baseUrl, key, properties.testTimeout());
            return new TestResult(true, (System.nanoTime() - started) / 1_000_000, models, null);
        } catch (RuntimeException exception) {
            return new TestResult(false, (System.nanoTime() - started) / 1_000_000, List.of(), friendly(provider, exception));
        }
    }

    // Model aliases each CLI accepts; the field stays free text (the runner validates the name).
    static List<String> cliModelSuggestions(String provider) {
        return ProviderUrlPolicy.CLAUDE_CLI.equals(provider) ? List.of("sonnet", "opus", "haiku") : List.of("default");
    }

    /** A CLI "test" proves the login works end to end: status, then one tiny real answer. */
    private TestResult probeCli(String provider, String model) {
        long started = System.nanoTime();
        if (cli == null || !cli.configured()) {
            return new TestResult(false, 0, List.of(), "o executor de CLIs (cli-runner) não está configurado neste gateway");
        }
        try {
            CliRunnerClient.EngineStatus status = cli.status(provider);
            if (!status.loggedIn()) {
                return new TestResult(false, (System.nanoTime() - started) / 1_000_000, List.of(),
                    "o CLI ainda não está logado — no servidor, rode: " + status.loginCommand());
            }
            var ping = new br.com.exotermo.prelo.gateway.llm.LLMRequest("cli-test",
                List.of(new br.com.exotermo.prelo.gateway.llm.LLMMessage("user", "Responda apenas: ok")), null, null, List.of());
            cli.chat(provider, "default".equals(model) ? null : model, ping, properties.cliTimeout());
            // Codex reports the account's real models ("default" = its top-priority one).
            List<String> models = new java.util.ArrayList<>(cliModelSuggestions(provider));
            status.models().stream().filter(m -> !models.contains(m)).forEach(models::add);
            return new TestResult(true, (System.nanoTime() - started) / 1_000_000, models, null);
        } catch (RuntimeException exception) {
            return new TestResult(false, (System.nanoTime() - started) / 1_000_000, List.of(), friendly(provider, exception));
        }
    }

    public CliStatus cliStatus() {
        if (cli == null || !cli.configured()) return new CliStatus(false, false, null, false, null);
        try {
            CliRunnerClient.EngineStatus claude = cli.status(ProviderUrlPolicy.CLAUDE_CLI);
            CliRunnerClient.EngineStatus codex = cli.status(ProviderUrlPolicy.CODEX_CLI);
            return new CliStatus(true, claude.loggedIn(), claude.loginCommand(), codex.loggedIn(), codex.loginCommand());
        } catch (RuntimeException exception) {
            return new CliStatus(false, false, null, false, null);
        }
    }

    private static String friendly(String provider, RuntimeException exception) {
        boolean isCli = ProviderUrlPolicy.isCli(provider);
        String message = exception.getMessage();
        if (isCli && message != null && message.startsWith(CliRunnerClient.RUNNER_MESSAGE)) {
            return message.substring(CliRunnerClient.RUNNER_MESSAGE.length());
        }
        return switch (exception) {
            case ProviderAuthenticationException ignored when isCli -> "o CLI não está logado (ou o login expirou) — faça login de novo no cli-runner";
            case ProviderQuotaExceededException ignored when isCli -> "o limite do seu plano foi atingido — tente de novo mais tarde";
            case ProviderQuotaExceededException ignored -> "a conta do provedor está sem crédito — adicione saldo no painel de cobrança dele";
            case ProviderAuthenticationException ignored -> "o provedor recusou a chave";
            case ProviderRateLimitedException ignored -> "o provedor limitou as requisições — tente de novo em instantes";
            case ProviderTimeoutException ignored -> "o provedor não respondeu a tempo";
            case ProviderUnavailableException ignored -> "não foi possível falar com o provedor";
            case ProviderRejectedRequestException ignored -> "o provedor recusou a requisição";
            default -> "falha inesperada ao testar a conexão";
        };
    }

    // --- writes ---

    @Transactional
    public ConnectionSummary save(UUID projectId, SaveCommand command) {
        requireEnabled();
        String provider = command.provider();
        if (!ProviderUrlPolicy.knownProvider(provider)) throw new ConnectionValidationException("provedor desconhecido");
        String model = blankToNull(command.model());
        if (model == null || model.length() > 200) throw new ConnectionValidationException("escolha um modelo");
        String baseUrl = ProviderUrlPolicy.resolveBaseUrl(provider, command.baseUrl(), properties.allowPrivateUrls());
        boolean isCli = ProviderUrlPolicy.isCli(provider);
        String newKey = isCli ? null : blankToNull(command.apiKey());
        if (newKey != null && newKey.length() > MAX_KEY_LENGTH) throw new ConnectionValidationException("chave de API longa demais");

        ProviderConnection connection = find(projectId).orElseGet(() -> new ProviderConnection(scopeOf(projectId), projectId));
        boolean providerChanged = connection.provider() != null && !connection.provider().equals(provider);
        String key;
        if (newKey != null) {
            key = newKey;
        } else if (isCli) {
            key = null;
        } else if (connection.hasKey() && !providerChanged) {
            key = cipher.open(connection.keySalt(), connection.encryptedKey(), connection.associatedData());
        } else if (connection.hasKey()) {
            // The sealed key is bound to its provider (associated data), so it can't follow a provider switch.
            throw new ConnectionValidationException("ao trocar de provedor, informe a chave do novo provedor");
        } else {
            key = null;
        }
        if (ProviderUrlPolicy.requiresKey(provider) && key == null) throw new ConnectionValidationException("informe a chave de API");

        TestResult test = probe(provider, baseUrl, key, model);
        if (!test.ok()) throw new ConnectionValidationException(test.error());
        if (!isCli && !test.models().isEmpty() && !test.models().contains(model)) {
            throw new ConnectionValidationException("esse modelo não está disponível para esta chave");
        }

        connection.configure(provider, baseUrl, model);
        if (key == null) {
            connection.clearKey();
        } else {
            connection.storeKey(cipher.seal(key, connection.associatedData()), last4(key));
        }
        connection.recordTest(true, test.latencyMs(), null);
        connection.setActive(true); // saving (re)activates — the instance one included, since Fase X
        return ConnectionSummary.of(repository.save(connection));
    }

    @Transactional
    public RetestResult retest(UUID projectId) {
        requireEnabled();
        ProviderConnection connection = find(projectId).orElseThrow(ConnectionNotFoundException::new);
        String key = connection.hasKey() ? cipher.open(connection.keySalt(), connection.encryptedKey(), connection.associatedData()) : null;
        TestResult test = probe(connection.provider(), connection.baseUrl(), key, connection.model());
        connection.recordTest(test.ok(), test.latencyMs(), test.error());
        return new RetestResult(ConnectionSummary.of(repository.save(connection)), test);
    }

    @Transactional
    public ConnectionSummary setActive(UUID projectId, boolean active) {
        requireEnabled();
        ProviderConnection connection = find(projectId).orElseThrow(ConnectionNotFoundException::new);
        connection.setActive(active);
        return ConnectionSummary.of(repository.save(connection));
    }

    @Transactional
    public void delete(UUID projectId) {
        requireEnabled();
        repository.delete(find(projectId).orElseThrow(ConnectionNotFoundException::new));
    }

    // --- usage (from the audit trail) ---

    public List<UsageDay> usage(UUID projectId, int days) {
        int span = Math.max(1, Math.min(days, 90));
        String filter = projectId == null ? "" : " AND project_id = ?";
        Object[] args = projectId == null ? new Object[] { span } : new Object[] { span, projectId };
        String sql = "SELECT (created_at AT TIME ZONE 'UTC')::date AS day, count(*) AS calls,"
            + " coalesce(sum(input_tokens), 0) AS input_tokens, coalesce(sum(output_tokens), 0) AS output_tokens"
            + " FROM gateway_audit_events"
            + " WHERE event_type = 'LLM_RESPONSE' AND created_at >= now() - make_interval(days => ?)" + filter
            + " GROUP BY day ORDER BY day";
        return jdbc.query(sql,
            (rs, row) -> new UsageDay(rs.getObject("day", LocalDate.class), rs.getLong("calls"), rs.getLong("input_tokens"), rs.getLong("output_tokens")),
            args);
    }

    // --- routing ---

    @Override
    @Transactional(readOnly = true)
    public Optional<RoutedConnection> route(UUID projectId, String origin) {
        if (cipher == null) return Optional.empty();
        // Fase X (hybrid use): a subscription CLI only serves work the owner started himself.
        boolean cliAllowed = "MANUAL".equals(origin);
        java.util.function.Predicate<ProviderConnection> usable =
            c -> c.active() && (cliAllowed || !ProviderUrlPolicy.isCli(c.provider()));
        Optional<ProviderConnection> chosen = Optional.empty();
        if (projectId != null) {
            chosen = repository.findByScopeAndProjectId(ProviderConnection.SCOPE_PROJECT, projectId).filter(usable);
        }
        if (chosen.isEmpty()) chosen = repository.findFirstByScope(ProviderConnection.SCOPE_INSTANCE).filter(usable);
        return chosen.map(connection -> {
            UUID id = connection.id();
            String provider = connection.provider();
            String baseUrl = connection.baseUrl();
            String model = connection.model();
            byte[] salt = connection.keySalt();
            byte[] sealed = connection.encryptedKey();
            String aad = connection.associatedData();
            return new RoutedConnection(provider, model, connection.scope(), request -> {
                try {
                    br.com.exotermo.prelo.gateway.llm.LLMResponse response = ProviderUrlPolicy.isCli(provider)
                        ? cli.chat(provider, "default".equals(model) ? null : model, request, properties.cliTimeout())
                        : wire.chat(provider, baseUrl, sealed == null ? null : cipher.open(salt, sealed, aad), model, request,
                            properties.maxTokens(), properties.chatTimeout());
                    recordCall(id, true, null);
                    return response;
                } catch (RuntimeException exception) {
                    recordCall(id, false, friendly(provider, exception));
                    throw exception;
                }
            });
        });
    }

    /** Outcome of the last real call — written outside the entity so it never fights the version. */
    private void recordCall(UUID id, boolean ok, String error) {
        if (jdbc == null) return;
        try {
            jdbc.update("UPDATE provider_connections SET last_call_at = now(), last_call_ok = ?, last_call_error = ? WHERE id = ?", ok, error, id);
        } catch (RuntimeException exception) {
            log.warn("could not record the last call of connection {}", id);
        }
    }

    private static String blankToNull(String value) {
        return value == null || value.isBlank() ? null : value.strip();
    }

    // Only reveal a suffix when the key is long enough that 4 characters say nothing useful.
    private static String last4(String key) {
        return key.length() >= 16 ? key.substring(key.length() - 4) : null;
    }
}
