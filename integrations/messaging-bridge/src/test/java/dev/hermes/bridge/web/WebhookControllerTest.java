package dev.hermes.bridge.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.mockito.Mockito.verifyNoInteractions;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.persistence.InboundEventStore;
import dev.hermes.bridge.service.AutoReplyGate;
import dev.hermes.bridge.service.InboundMessageHandler;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.HexFormat;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import static org.mockito.Mockito.mock;

class WebhookControllerTest {
    private static final String SECRET = "test-secret";

    @Test
    void defaultsToPausedAndDoesNotCallHermesOrMessagingCoreHandler() throws Exception {
        BridgeProperties properties = new BridgeProperties("", "", "", SECRET, "", false, "admin", List.of());
        AutoReplyGate gate = new AutoReplyGate(properties);
        InboundMessageHandler handler = mock(InboundMessageHandler.class);
        InboundEventStore events = mock(InboundEventStore.class);
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), handler, gate, events);
        String body = "{\"eventType\":\"inbound_message\",\"channelIdentityId\":\"channel\",\"messageId\":\"message\",\"from\":\"5511999999999\",\"text\":\"hello\"}";

        assertEquals(HttpStatus.ACCEPTED, controller.receive("sha256=" + hmac(body), body).getStatusCode());
        verifyNoInteractions(handler);
    }

    @Test
    void runtimePauseImmediatelyStopsAStaticallyEnabledBridge() throws Exception {
        BridgeProperties properties = new BridgeProperties("", "", "", SECRET, "", true, "admin", List.of());
        AutoReplyGate gate = new AutoReplyGate(properties);
        new AdminController(properties, gate).pause("admin");
        InboundMessageHandler handler = mock(InboundMessageHandler.class);
        InboundEventStore events = mock(InboundEventStore.class);
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), handler, gate, events);
        String body = "{\"eventType\":\"inbound_message\",\"channelIdentityId\":\"channel\",\"messageId\":\"message\",\"from\":\"5511999999999\",\"text\":\"hello\"}";

        assertEquals(HttpStatus.ACCEPTED, controller.receive("sha256=" + hmac(body), body).getStatusCode());
        verifyNoInteractions(handler);
    }

    @Test
    void rejectsV2CallbackWithExpiredTimestampBeforeParsing() throws Exception {
        BridgeProperties properties = new BridgeProperties("", "", "", SECRET, "", false, "admin", List.of(), 300, false);
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), mock(InboundMessageHandler.class),
            new AutoReplyGate(properties), mock(InboundEventStore.class));
        String body = "{\"not\":\"processed\"}";
        String timestamp = Long.toString(Instant.now().minusSeconds(301).getEpochSecond());
        assertEquals(HttpStatus.UNAUTHORIZED, controller.receive("sha256=" + hmac(timestamp + "." + body), timestamp,
            "webhook-1", "request-1", body).getStatusCode());
    }

    private static String hmac(String body) throws Exception {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(SECRET.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        return HexFormat.of().formatHex(mac.doFinal(body.getBytes(StandardCharsets.UTF_8)));
    }

}
