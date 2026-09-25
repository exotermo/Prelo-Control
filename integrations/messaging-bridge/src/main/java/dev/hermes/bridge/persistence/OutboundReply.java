package dev.hermes.bridge.persistence;

import java.time.Instant;
import java.util.UUID;

// One row per WhatsApp reply the bridge owes messaging-core. messageId (the original inbound
// message's id) doubles as the clientMessageId sent to POST /api/v1/messages, so a retried send
// after a crash — before this row is marked SENT — cannot create a duplicate OutboundMessage on
// the messaging-core side (see that repo's SendMessageUseCase clientMessageId handling).
public record OutboundReply(UUID id, String messageId, String channelIdentityId, String toAddress, String text,
                             OutboundReplyStatus status, int attempt, int maxAttempts, Instant availableAt,
                             String claimedBy, Instant claimedAt, Instant leaseExpiresAt, String lastError,
                             Instant createdAt, Instant updatedAt, long version) {

    public static final int DEFAULT_MAX_ATTEMPTS = 5;

    public static OutboundReply pending(String messageId, String channelIdentityId, String toAddress, String text) {
        Instant now = Instant.now();
        return new OutboundReply(UUID.randomUUID(), messageId, channelIdentityId, toAddress, text,
            OutboundReplyStatus.PENDING, 0, DEFAULT_MAX_ATTEMPTS, now, null, null, null, null, now, now, 0L);
    }
}
