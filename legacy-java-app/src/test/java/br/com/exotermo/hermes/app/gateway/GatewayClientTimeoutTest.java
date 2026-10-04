package br.com.exotermo.hermes.app.gateway;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.net.ServerSocket;
import java.util.List;
import org.junit.jupiter.api.Test;

class GatewayClientTimeoutTest {

    @Test void failsFastInsteadOfBlockingForeverWhenTheGatewayNeverResponds() throws Exception {
        try (ServerSocket serverSocket = new ServerSocket(0)) {
            Thread stalledServer = new Thread(() -> {
                try (var socket = serverSocket.accept()) {
                    Thread.sleep(30_000);
                } catch (Exception ignored) { }
            });
            stalledServer.setDaemon(true);
            stalledServer.start();

            var properties = new GatewayProperties("http://127.0.0.1:" + serverSocket.getLocalPort(),
                "secret-at-least-32-bytes-long-0000", "hermes-dev", "llm-gateway");
            var client = new GatewayClient(properties, new ObjectMapper());
            var request = new GatewayContracts.ChatRequest("mock-echo", List.of(new GatewayContracts.Message("user", "hi")), null, null);

            long started = System.currentTimeMillis();
            GatewayCallException exception = assertThrows(GatewayCallException.class, () -> client.chat(request, "req-1"));
            long elapsedMs = System.currentTimeMillis() - started;

            assertEquals(504, exception.status());
            assertTrue(elapsedMs < 15_000, "expected the client's own timeout to trip well before 15s, took " + elapsedMs + "ms");
        }
    }
}
