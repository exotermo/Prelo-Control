package br.com.exotermo.prelo.gateway.connection;

import br.com.exotermo.prelo.gateway.llm.LLMRequest;
import br.com.exotermo.prelo.gateway.llm.LLMResponse;
import java.util.Optional;
import java.util.UUID;
import java.util.function.Function;

/**
 * Picks the dashboard-managed connection for a request: the project's own active connection,
 * else the instance default, else nothing (the caller then falls back to the static profiles).
 * The provider key is decrypted only inside {@code call}, for the duration of one request.
 */
public interface ConnectionRouter {
    Optional<RoutedConnection> route(UUID projectId);

    record RoutedConnection(String provider, String model, String scope, Function<LLMRequest, LLMResponse> call) { }

    ConnectionRouter NONE = projectId -> Optional.empty();
}
