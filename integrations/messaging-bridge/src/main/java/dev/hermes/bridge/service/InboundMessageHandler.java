package dev.hermes.bridge.service;

import dev.hermes.bridge.client.HermesAppClient;
import dev.hermes.bridge.persistence.InboundEventStatus;
import dev.hermes.bridge.persistence.InboundEventStore;
import dev.hermes.bridge.persistence.OutboundReply;
import dev.hermes.bridge.persistence.OutboundReplyStore;
import dev.hermes.bridge.persistence.OwnerContactStore;
import dev.hermes.bridge.web.InboundMessageEvent;
import java.time.Duration;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

// Orchestrates the whole bridge: an inbound WhatsApp message (already verified, parsed and
// durably recorded by WebhookController) goes to hermes-app-go as a Task, and whatever the agent
// produces is queued in outbound_replies for OutboundReplyWorker to actually deliver — this
// method never calls messaging-core itself, so a crash after the execution completes cannot lose
// the reply (Fase F point 4). Runs off the request thread (@Async) since the webhook already
// answered 202 before this starts.
@Service
public class InboundMessageHandler {
    private static final Logger log = LoggerFactory.getLogger(InboundMessageHandler.class);
    private static final Duration EXECUTION_TIMEOUT = Duration.ofSeconds(30);
    private static final String OWNER_AGENT_ID = "general";
    private static final String CUSTOMER_AGENT_ID = "customer";

    private final HermesAppClient hermesAppClient;
    private final AutoReplyGate autoReplyGate;
    private final OwnerContactStore ownerContacts;
    private final InboundEventStore events;
    private final OutboundReplyStore replies;

    public InboundMessageHandler(HermesAppClient hermesAppClient, AutoReplyGate autoReplyGate,
                                  OwnerContactStore ownerContacts, InboundEventStore events, OutboundReplyStore replies) {
        this.hermesAppClient = hermesAppClient;
        this.autoReplyGate = autoReplyGate;
        this.ownerContacts = ownerContacts;
        this.events = events;
        this.replies = replies;
    }

    @Async
    public void handleAsync(InboundMessageEvent event) {
        handle(event);
    }

    void handle(InboundMessageEvent event) {
        if (!autoReplyGate.isEnabled()) {
            log.warn("auto_reply_paused messageId={} channelIdentityId={} before=hermes", event.messageId(), event.channelIdentityId());
            events.markStatus(event.messageId(), InboundEventStatus.FAILED);
            return;
        }
        events.markStatus(event.messageId(), InboundEventStatus.PROCESSING);

        // Sender→authorization mapping (Fase F point 4): a contact that is not a known owner
        // number never gets the "general" agent's capabilities (delegate_to_agent, tools) —
        // it gets "customer", which the catalog defines with none.
        String agentId = ownerContacts.contains(event.from()) ? OWNER_AGENT_ID : CUSTOMER_AGENT_ID;
        HermesAppClient.ExecutionResult result = hermesAppClient.run(event.text(), agentId, EXECUTION_TIMEOUT);

        if (!"COMPLETED".equals(result.status()) || result.result() == null || result.result().isBlank()) {
            log.warn("no reply sent for message {}: hermes-app execution ended as {} ({})",
                event.messageId(), result.status(), result.error());
            events.markStatus(event.messageId(), InboundEventStatus.FAILED);
            return;
        }

        replies.insert(OutboundReply.pending(event.messageId(), event.channelIdentityId(), event.from(), result.result()));
        events.markStatus(event.messageId(), InboundEventStatus.DONE);
        log.info("reply_queued contactHash={} channelIdentityId={} taskId={} agentId={} responseHash={}",
            shortHash(event.from()), event.channelIdentityId(), result.taskId(), agentId, shortHash(result.result()));
    }

    private static String shortHash(String value) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8));
            return HexFormat.of().formatHex(digest, 0, 8);
        } catch (Exception exception) {
            return "unavailable";
        }
    }
}
