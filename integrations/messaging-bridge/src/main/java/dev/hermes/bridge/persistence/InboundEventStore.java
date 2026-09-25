package dev.hermes.bridge.persistence;

import java.sql.Timestamp;
import java.time.Instant;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Component;
import org.springframework.scheduling.annotation.Scheduled;

// Message-level idempotency + "persist before ack": a row here is inserted inside
// WebhookController.receive, before it returns 202, and a duplicate messageId (a redelivery from
// messaging-core's own callback retry budget) hits the unique constraint instead of dispatching a
// second Task. Mirrors the durable-job-queue convention used across the repo family (Hermes'
// execution_jobs, messaging-core's outbound_jobs/callback_deliveries) at the smallest scale that
// need justifies here — two states of interest (seen or not), not a claim/lease queue.
@Component
public class InboundEventStore {
    private final JdbcTemplate jdbc;

    public InboundEventStore(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    // Returns true if this messageId was recorded for the first time; false if it was already
    // known (a redelivery) — the caller must not dispatch again in that case.
    public boolean insertIfAbsent(String messageId, String channelIdentityId, String fromAddress, String rawPayload) {
        try {
            jdbc.update("""
                INSERT INTO inbound_events (message_id, channel_identity_id, from_address, raw_payload, status, received_at)
                VALUES (?, ?, ?, ?, ?, ?)
                """, messageId, channelIdentityId, fromAddress, rawPayload, InboundEventStatus.RECEIVED.name(), Timestamp.from(Instant.now()));
            return true;
        } catch (DuplicateKeyException alreadySeen) {
            return false;
        }
    }

    /** Atomically reserves a v2 webhook id before JSON parsing or business dispatch. */
    public boolean registerWebhookIfAbsent(String webhookId, Instant receivedAt, Instant expiresAt, String requestId) {
        if (webhookId == null || webhookId.isBlank()) return false;
        try {
            jdbc.update("""
                INSERT INTO webhook_replays (webhook_id, received_at, expires_at, request_id)
                VALUES (?, ?, ?, ?)
                """, webhookId, Timestamp.from(receivedAt), Timestamp.from(expiresAt), requestId);
            return true;
        } catch (DuplicateKeyException alreadySeen) {
            return false;
        }
    }

    @Scheduled(fixedDelayString = "${bridge.replay-cleanup-ms:3600000}")
    public void purgeExpiredWebhookReplays() {
        jdbc.update("DELETE FROM webhook_replays WHERE expires_at < ?", Timestamp.from(Instant.now()));
    }

    public void markStatus(String messageId, InboundEventStatus status) {
        jdbc.update("UPDATE inbound_events SET status = ?, processed_at = ? WHERE message_id = ?",
            status.name(), Timestamp.from(Instant.now()), messageId);
    }
}
