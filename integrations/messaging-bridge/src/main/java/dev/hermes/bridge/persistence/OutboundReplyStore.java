package dev.hermes.bridge.persistence;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Timestamp;
import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.UUID;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Component;

// Same claim/lease/retry shape as messaging-core's JdbcOutboundJobStore and hermes-app-go's
// execution_jobs (ADR-013 there): status-guarded atomic UPDATE resolves the claim race entirely
// in Postgres, no in-memory worker registry.
@Component
public class OutboundReplyStore {
    private final JdbcTemplate jdbc;

    public OutboundReplyStore(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(OutboundReply reply) {
        jdbc.update("""
            INSERT INTO outbound_replies
                (id, message_id, channel_identity_id, to_address, text, status, attempt, max_attempts,
                 available_at, claimed_by, claimed_at, lease_expires_at, last_error, created_at, updated_at, version)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            """, reply.id(), reply.messageId(), reply.channelIdentityId(), reply.toAddress(), reply.text(),
            reply.status().name(), reply.attempt(), reply.maxAttempts(), Timestamp.from(reply.availableAt()),
            reply.claimedBy(), reply.claimedAt() == null ? null : Timestamp.from(reply.claimedAt()),
            reply.leaseExpiresAt() == null ? null : Timestamp.from(reply.leaseExpiresAt()), reply.lastError(),
            Timestamp.from(reply.createdAt()), Timestamp.from(reply.updatedAt()), reply.version());
    }

    public List<UUID> listClaimable(int limit) {
        return jdbc.query("""
            SELECT id FROM outbound_replies
             WHERE status IN ('PENDING','RETRY') AND available_at <= now()
             ORDER BY created_at ASC
             LIMIT ?
            """, (rs, rowNum) -> rs.getObject("id", UUID.class), limit);
    }

    public java.util.Optional<OutboundReply> claim(UUID id, String workerId, Duration leaseDuration) {
        Instant now = Instant.now();
        Instant leaseExpiresAt = now.plus(leaseDuration);
        return jdbc.query("""
            UPDATE outbound_replies
               SET status = 'CLAIMED', claimed_by = ?, claimed_at = ?, lease_expires_at = ?, updated_at = ?, version = version + 1
             WHERE id = ? AND status IN ('PENDING','RETRY')
             RETURNING id, message_id, channel_identity_id, to_address, text, status, attempt, max_attempts,
                       available_at, claimed_by, claimed_at, lease_expires_at, last_error, created_at, updated_at, version
            """, OutboundReplyStore::map, workerId, Timestamp.from(now), Timestamp.from(leaseExpiresAt),
            Timestamp.from(now), id).stream().findFirst();
    }

    public void markRunning(UUID id) {
        jdbc.update("UPDATE outbound_replies SET status = 'RUNNING', updated_at = now(), version = version + 1 WHERE id = ?", id);
    }

    public void markSent(UUID id) {
        jdbc.update("UPDATE outbound_replies SET status = 'SENT', updated_at = now(), version = version + 1 WHERE id = ?", id);
    }

    public OutboundReplyStatus retryOrDead(UUID id, String message) {
        String status = jdbc.queryForObject("""
            UPDATE outbound_replies
               SET status = CASE WHEN attempt + 1 >= max_attempts THEN 'DEAD' ELSE 'RETRY' END,
                   attempt = attempt + 1,
                   available_at = now() + (LEAST(attempt + 1, 6) * interval '10 seconds'),
                   last_error = ?,
                   updated_at = now(),
                   version = version + 1
             WHERE id = ?
             RETURNING status
            """, String.class, message, id);
        return OutboundReplyStatus.valueOf(status);
    }

    // Crash recovery: replies stuck CLAIMED/RUNNING past their lease get requeued — same
    // sweeper pattern as OutboundWorker.tick()/execution_jobs' sweeper.
    public List<UUID> requeueOrphaned() {
        return jdbc.query("""
            UPDATE outbound_replies
               SET status = CASE WHEN attempt + 1 >= max_attempts THEN 'DEAD' ELSE 'RETRY' END,
                   attempt = attempt + 1,
                   available_at = now() + (LEAST(attempt + 1, 6) * interval '10 seconds'),
                   last_error = 'lease expired, requeued by sweeper',
                   updated_at = now(),
                   version = version + 1
             WHERE status IN ('CLAIMED','RUNNING') AND lease_expires_at < now()
             RETURNING id, status
            """, (rs, rowNum) -> "RETRY".equals(rs.getString("status")) ? rs.getObject("id", UUID.class) : null)
            .stream().filter(java.util.Objects::nonNull).toList();
    }

    private static OutboundReply map(ResultSet rs, int rowNum) throws SQLException {
        Timestamp claimedAt = rs.getTimestamp("claimed_at");
        Timestamp leaseExpiresAt = rs.getTimestamp("lease_expires_at");
        return new OutboundReply(
            rs.getObject("id", UUID.class), rs.getString("message_id"), rs.getString("channel_identity_id"),
            rs.getString("to_address"), rs.getString("text"), OutboundReplyStatus.valueOf(rs.getString("status")),
            rs.getInt("attempt"), rs.getInt("max_attempts"), rs.getTimestamp("available_at").toInstant(),
            rs.getString("claimed_by"), claimedAt == null ? null : claimedAt.toInstant(),
            leaseExpiresAt == null ? null : leaseExpiresAt.toInstant(), rs.getString("last_error"),
            rs.getTimestamp("created_at").toInstant(), rs.getTimestamp("updated_at").toInstant(), rs.getLong("version"));
    }
}
