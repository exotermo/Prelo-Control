package dev.hermes.bridge.web;

import com.fasterxml.jackson.databind.ObjectMapper;
import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.persistence.InboundEventStore;
import dev.hermes.bridge.service.InboundMessageHandler;
import dev.hermes.bridge.service.AutoReplyGate;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RestController;

// Never processes a body before verifying its signature — see HmacVerifier. Responds fast
// (202, before the LLM/WhatsApp round trip) because messaging-core's own callback delivery
// has a bounded retry budget (see that repo's DeliverCallbackUseCase); a slow response here
// would just burn those retries for no reason.
@RestController
public class WebhookController {
    private static final Logger log = LoggerFactory.getLogger(WebhookController.class);

    private final BridgeProperties properties;
    private final ObjectMapper objectMapper;
    private final InboundMessageHandler handler;
    private final AutoReplyGate autoReplyGate;
    private final InboundEventStore events;

    public WebhookController(BridgeProperties properties, ObjectMapper objectMapper, InboundMessageHandler handler,
                             AutoReplyGate autoReplyGate, InboundEventStore events) {
        this.properties = properties;
        this.objectMapper = objectMapper;
        this.handler = handler;
        this.autoReplyGate = autoReplyGate;
        this.events = events;
    }

    @PostMapping("/webhooks/messaging-core")
    public ResponseEntity<Void> receive(
            @RequestHeader(value = "X-Signature", required = false) String signature,
            @RequestHeader(value = "X-Webhook-Timestamp", required = false) String timestamp,
            @RequestHeader(value = "X-Webhook-Id", required = false) String webhookId,
            @RequestHeader(value = "X-Request-Id", required = false) String requestId,
            @RequestBody String rawBody) {
        byte[] bodyBytes = rawBody.getBytes(StandardCharsets.UTF_8);
        boolean v2 = timestamp != null || webhookId != null;
        if (v2) {
            Duration tolerance = Duration.ofSeconds(properties.callbackReplayToleranceSeconds());
            if (webhookId == null || webhookId.isBlank() || webhookId.length() > 200
                || !HmacVerifier.verifyV2(properties.callbackSigningSecret(), bodyBytes, timestamp, signature, tolerance)) {
                log.warn("rejected callback: missing/invalid v2 signature or timestamp");
                return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
            }
            Instant now = Instant.now();
            if (!events.registerWebhookIfAbsent(webhookId.trim(), now, now.plus(tolerance), requestId)) {
                log.info("duplicate_callback webhookId={} — not dispatched again", webhookId);
                return ResponseEntity.accepted().build();
            }
        } else if (!properties.allowLegacyCallbackSignatures()
            || !HmacVerifier.verify(properties.callbackSigningSecret(), bodyBytes, signature)) {
            log.warn("rejected callback: missing or invalid callback signature");
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
        }

        InboundMessageEvent event;
        try {
            event = objectMapper.readValue(rawBody, InboundMessageEvent.class);
        } catch (Exception exception) {
            log.warn("rejected callback: malformed JSON body");
            return ResponseEntity.badRequest().build();
        }

        if (!autoReplyGate.isEnabled()) {
            log.warn("auto_reply_paused messageId={} channelIdentityId={} staticEnabled={} runtimeEnabled={}",
                event.messageId(), event.channelIdentityId(), properties.autoReplyEnabled(), autoReplyGate.isRuntimeEnabled());
            return ResponseEntity.accepted().build();
        }

        // Persist before ack (Fase F): a duplicate messageId means this is a redelivery from
        // messaging-core's own callback retry budget — already recorded, do not dispatch again.
        if (!events.insertIfAbsent(event.messageId(), event.channelIdentityId(), event.from(), rawBody)) {
            log.info("duplicate_inbound_event messageId={} — not dispatched again", event.messageId());
            return ResponseEntity.accepted().build();
        }

        handler.handleAsync(event);
        return ResponseEntity.accepted().build();
    }

    // Source compatibility for unit callers written before contract v2. Production HTTP traffic
    // always enters the annotated method above and therefore follows the configured policy.
    public ResponseEntity<Void> receive(String signature, String rawBody) {
        return receive(signature, null, null, null, rawBody);
    }
}
