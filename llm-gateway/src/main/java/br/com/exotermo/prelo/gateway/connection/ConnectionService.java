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
                                    Long lastTestLatencyMs, String lastTestError, Instant updatedAt) {
        static ConnectionSummary empty(String scope, UUID projectId) {
            return new ConnectionSummary(false, scope, projectId == null ? null : projectId.toString(), null, null, null,
                false, false, null, null, null, null, null, null);
        }

        static ConnectionSummary of(ProviderConnection c) {
            return new ConnectionSummary(true, c.scope(), c.projectId() == null ? null : c.projectId().toString(), c.provider(), c.baseUrl(),
                c.model(), c.active(), c.hasKey(), c.keyLast4(), c.lastTestAt(), c.lastTestOk(), c.lastTestLatencyMs(), c.lastTestError(), c.updatedAt());
        }
    }

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

    @Autowired
    public ConnectionService(ProviderConnectionRepository repository, ConnectionProperties properties, ObjectMapper objectMapper, JdbcTemplate jdbc) {
        this(repository, properties, new ProviderWireClient(objectMapper), jdbc, loadCipher(properties.masterKeyFile()));
    }

    ConnectionService(ProviderConnectionRepository repository, ConnectionProperties properties, ProviderWireClient wire, JdbcTemplate jdbc, ConnectionKeyCipher cipher) {
        this.repository = repository;
        this.properties = properties;
        this.wire = wire;
        this.jdbc = jdbc;
        this.cipher = cipher;
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
        return probe(provider, resolvedUrl, key);
    }

    private TestResult probe(String provider, String baseUrl, String key) {
        long started = System.nanoTime();
        try {
            List<String> models = wire.listModels(provider, baseUrl, key, properties.testTimeout());
            return new TestResult(true, (System.nanoTime() - started) / 1_000_000, models, null);
        } catch (RuntimeException exception) {
            return new TestResult(false, (System.nanoTime() - started) / 1_000_000, List.of(), friendly(exception));
        }
    }

    private static String friendly(RuntimeException exception) {
        return switch (exception) {
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
        String newKey = blankToNull(command.apiKey());
        if (newKey != null && newKey.length() > MAX_KEY_LENGTH) throw new ConnectionValidationException("chave de API longa demais");

        ProviderConnection connection = find(projectId).orElseGet(() -> new ProviderConnection(scopeOf(projectId), projectId));
        boolean providerChanged = connection.provider() != null && !connection.provider().equals(provider);
        String key;
        if (newKey != null) {
            key = newKey;
        } else if (connection.hasKey() && !providerChanged) {
            key = cipher.open(connection.keySalt(), connection.encryptedKey(), connection.associatedData());
        } else if (connection.hasKey()) {
            // The sealed key is bound to its provider (associated data), so it can't follow a provider switch.
            throw new ConnectionValidationException("ao trocar de provedor, informe a chave do novo provedor");
        } else {
            key = null;
        }
        if (ProviderUrlPolicy.requiresKey(provider) && key == null) throw new ConnectionValidationException("informe a chave de API");

        TestResult test = probe(provider, baseUrl, key);
        if (!test.ok()) throw new ConnectionValidationException(test.error());
        if (!test.models().isEmpty() && !test.models().contains(model)) {
            throw new ConnectionValidationException("esse modelo não está disponível para esta chave");
        }

        connection.configure(provider, baseUrl, model);
        if (key == null) {
            connection.clearKey();
        } else {
            connection.storeKey(cipher.seal(key, connection.associatedData()), last4(key));
        }
        connection.recordTest(true, test.latencyMs(), null);
        if (projectId != null) connection.setActive(true);
        return ConnectionSummary.of(repository.save(connection));
    }

    @Transactional
    public RetestResult retest(UUID projectId) {
        requireEnabled();
        ProviderConnection connection = find(projectId).orElseThrow(ConnectionNotFoundException::new);
        String key = connection.hasKey() ? cipher.open(connection.keySalt(), connection.encryptedKey(), connection.associatedData()) : null;
        TestResult test = probe(connection.provider(), connection.baseUrl(), key);
        connection.recordTest(test.ok(), test.latencyMs(), test.error());
        return new RetestResult(ConnectionSummary.of(repository.save(connection)), test);
    }

    @Transactional
    public ConnectionSummary setActive(UUID projectId, boolean active) {
        requireEnabled();
        if (projectId == null) throw new ConnectionValidationException("a conexão da instância está sempre ativa");
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
    public Optional<RoutedConnection> route(UUID projectId) {
        if (cipher == null) return Optional.empty();
        Optional<ProviderConnection> chosen = Optional.empty();
        if (projectId != null) {
            chosen = repository.findByScopeAndProjectId(ProviderConnection.SCOPE_PROJECT, projectId).filter(ProviderConnection::active);
        }
        if (chosen.isEmpty()) chosen = repository.findFirstByScope(ProviderConnection.SCOPE_INSTANCE);
        return chosen.map(connection -> {
            String provider = connection.provider();
            String baseUrl = connection.baseUrl();
            String model = connection.model();
            byte[] salt = connection.keySalt();
            byte[] sealed = connection.encryptedKey();
            String aad = connection.associatedData();
            return new RoutedConnection(provider, model, connection.scope(), request -> {
                String key = sealed == null ? null : cipher.open(salt, sealed, aad);
                return wire.chat(provider, baseUrl, key, model, request, properties.maxTokens(), properties.chatTimeout());
            });
        });
    }

    private static String blankToNull(String value) {
        return value == null || value.isBlank() ? null : value.strip();
    }

    // Only reveal a suffix when the key is long enough that 4 characters say nothing useful.
    private static String last4(String key) {
        return key.length() >= 16 ? key.substring(key.length() - 4) : null;
    }
}
