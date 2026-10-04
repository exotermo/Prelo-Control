package dev.prelo.bridge.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoInteractions;
import static org.mockito.Mockito.when;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.prelo.bridge.client.MessagingCoreClient;
import dev.prelo.bridge.config.BridgeProperties;
import dev.prelo.bridge.persistence.InboundEventStore;
import dev.prelo.bridge.persistence.OwnerContactStore;
import dev.prelo.bridge.service.AutoReplyGate;
import dev.prelo.bridge.service.InboundMessageHandler;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.HexFormat;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.springframework.http.HttpStatus;
import static org.mockito.Mockito.mock;

class WebhookControllerTest {
    private static final String SECRET = "test-secret";

    @Test
    void defaultsToPausedAndDoesNotCallPreloOrMessagingCoreHandler() throws Exception {
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
        new AdminController(properties, gate, mock(OwnerContactStore.class), mock(MessagingCoreClient.class)).pause("admin");
        InboundMessageHandler handler = mock(InboundMessageHandler.class);
        InboundEventStore events = mock(InboundEventStore.class);
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), handler, gate, events);
        String body = "{\"eventType\":\"inbound_message\",\"channelIdentityId\":\"channel\",\"messageId\":\"message\",\"from\":\"5511999999999\",\"text\":\"hello\"}";

        assertEquals(HttpStatus.ACCEPTED, controller.receive("sha256=" + hmac(body), body).getStatusCode());
        verifyNoInteractions(handler);
    }

    @Test
    void rejectsV2CallbackWithExpiredTimestampBeforeParsing() throws Exception {
        BridgeProperties properties = new BridgeProperties("", "", "", SECRET, "", false, "admin", List.of(), 300, false, null, "messaging-core", "messaging-core");
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), mock(InboundMessageHandler.class),
            new AutoReplyGate(properties), mock(InboundEventStore.class));
        String body = "{\"not\":\"processed\"}";
        String timestamp = Long.toString(Instant.now().minusSeconds(301).getEpochSecond());
        assertEquals(HttpStatus.UNAUTHORIZED, controller.receive("sha256=" + hmac(timestamp + "." + body), timestamp,
            "webhook-1", "request-1", body).getStatusCode());
    }

    // Regression test for the 2026-10-01 incident: messaging-core's contractVersion>=2 callback
    // sends the nested MESSAGE_RECEIVED envelope (JdbcCallbackEventPublisher.envelope), not the
    // old flat "inbound_message" shape — a real inbound WhatsApp message silently failed to
    // persist ("null value in column message_id") until InboundMessageEvent learned to parse it.
    @Test
    void acceptsAV2MessageReceivedEnvelopeAndDispatchesTheParsedEvent() throws Exception {
        BridgeProperties properties = new BridgeProperties("", "", "", SECRET, "", true, "admin", List.of(), 300, false, null, "messaging-core", "messaging-core");
        AutoReplyGate gate = new AutoReplyGate(properties);
        InboundMessageHandler handler = mock(InboundMessageHandler.class);
        InboundEventStore events = mock(InboundEventStore.class);
        when(events.registerWebhookIfAbsent(anyString(), any(), any(), anyString())).thenReturn(true);
        when(events.insertIfAbsent(anyString(), anyString(), anyString(), anyString())).thenReturn(true);
        WebhookController controller = new WebhookController(properties, new ObjectMapper(), handler, gate, events);
        String body = "{\"eventId\":\"evt-1\",\"eventType\":\"MESSAGE_RECEIVED\",\"tenantId\":\"tenant-1\",\"integrationId\":null,"
            + "\"requestId\":\"req-1\",\"channel\":{\"id\":\"channel-1\",\"type\":\"WHATSAPP\"},"
            + "\"contact\":{\"id\":\"contact-1\",\"externalId\":\"+5511999999999\"},"
            + "\"message\":{\"id\":\"msg-1\",\"text\":\"hello\",\"receivedAt\":\"2026-10-01T00:00:00Z\"}}";
        String timestamp = Long.toString(Instant.now().getEpochSecond());

        var response = controller.receive("sha256=" + hmac(timestamp + "." + body), timestamp, "webhook-1", "request-1", body);

        assertEquals(HttpStatus.ACCEPTED, response.getStatusCode());
        ArgumentCaptor<InboundMessageEvent> captor = ArgumentCaptor.forClass(InboundMessageEvent.class);
        verify(handler).handleAsync(captor.capture());
        assertEquals("msg-1", captor.getValue().messageId());
        assertEquals("channel-1", captor.getValue().channelIdentityId());
        assertEquals("+5511999999999", captor.getValue().from());
        assertEquals("hello", captor.getValue().text());
    }

    private static String hmac(String body) throws Exception {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(SECRET.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        return HexFormat.of().formatHex(mac.doFinal(body.getBytes(StandardCharsets.UTF_8)));
    }

}
