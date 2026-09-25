package dev.hermes.bridge.service;

import dev.hermes.bridge.client.MessagingCoreClient;
import dev.hermes.bridge.persistence.OutboundReply;
import dev.hermes.bridge.persistence.OutboundReplyStatus;
import dev.hermes.bridge.persistence.OutboundReplyStore;
import java.time.Duration;
import java.util.List;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

// Drains outbound_replies (Fase F's outbox): a reply written by InboundMessageHandler survives
// a bridge restart because it is a row, not a direct call — a crash between "execution completed"
// and "sent" now just leaves a PENDING/CLAIMED row for this worker to pick up again, instead of
// silently losing the answer. Same fixed-delay-poll shape as messaging-core's OutboundWorker.
@Component
public class OutboundReplyWorker {
    private static final Logger log = LoggerFactory.getLogger(OutboundReplyWorker.class);
    private static final Duration LEASE_DURATION = Duration.ofMinutes(2);
    private static final int BATCH_SIZE = 20;
    private static final String WORKER_ID = "bridge-outbound-worker";

    private final OutboundReplyStore replies;
    private final MessagingCoreClient messagingCoreClient;
    private final AutoReplyGate autoReplyGate;

    public OutboundReplyWorker(OutboundReplyStore replies, MessagingCoreClient messagingCoreClient, AutoReplyGate autoReplyGate) {
        this.replies = replies;
        this.messagingCoreClient = messagingCoreClient;
        this.autoReplyGate = autoReplyGate;
    }

    @Scheduled(fixedDelay = 2000)
    public void tick() {
        try {
            List<UUID> requeued = replies.requeueOrphaned();
            if (!requeued.isEmpty()) log.warn("requeued {} orphaned outbound repl(y/ies)", requeued.size());
        } catch (RuntimeException exception) {
            log.error("outbound reply worker: requeueOrphaned failed", exception);
        }

        List<UUID> claimable;
        try {
            claimable = replies.listClaimable(BATCH_SIZE);
        } catch (RuntimeException exception) {
            log.error("outbound reply worker: listClaimable failed", exception);
            return;
        }
        for (UUID id : claimable) {
            try {
                process(id);
            } catch (RuntimeException exception) {
                log.error("outbound reply worker: processing {} failed", id, exception);
            }
        }
    }

    private void process(UUID id) {
        OutboundReply claimed = replies.claim(id, WORKER_ID, LEASE_DURATION).orElse(null);
        if (claimed == null) return; // lost the race to another worker, or no longer claimable
        if (!autoReplyGate.isEnabled()) {
            log.warn("auto_reply_paused messageId={} — not sent, requeued for later", claimed.messageId());
            replies.retryOrDead(id, "auto-reply paused");
            return;
        }
        replies.markRunning(id);

        try {
            messagingCoreClient.sendMessage(claimed.channelIdentityId(), claimed.toAddress(), claimed.text(), claimed.messageId());
            replies.markSent(id);
            log.info("outbound_reply_sent messageId={} attempt={}", claimed.messageId(), claimed.attempt() + 1);
        } catch (RuntimeException exception) {
            OutboundReplyStatus outcome = replies.retryOrDead(id, exception.getMessage());
            if (outcome == OutboundReplyStatus.DEAD) {
                log.error("outbound_reply_dead messageId={} after {} attempts", claimed.messageId(), claimed.attempt() + 1, exception);
            }
        }
    }
}
