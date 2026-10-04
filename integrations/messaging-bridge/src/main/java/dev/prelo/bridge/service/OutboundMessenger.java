package dev.prelo.bridge.service;

import dev.prelo.bridge.client.MessagingCoreClient;
import dev.prelo.bridge.contact.ContactAddress;
import dev.prelo.bridge.persistence.OutboundReply;
import dev.prelo.bridge.persistence.OutboundReplyStore;
import dev.prelo.bridge.persistence.OwnerContactStore;
import java.util.List;
import java.util.UUID;
import org.springframework.stereotype.Service;

/**
 * Fase T: messages the bridge sends on its own initiative (not as a reply to an inbound one) —
 * an approved agent message to a client, and approval requests to the owners. They go through the
 * same durable outbox as replies (OutboundReplyWorker), on the connected WhatsApp channel.
 */
@Service
public class OutboundMessenger {
    public static class NoConnectedChannelException extends RuntimeException {
        public NoConnectedChannelException() { super("no connected WhatsApp channel"); }
    }

    private final MessagingCoreClient messagingCore;
    private final OutboundReplyStore replies;
    private final OwnerContactStore owners;

    public OutboundMessenger(MessagingCoreClient messagingCore, OutboundReplyStore replies, OwnerContactStore owners) {
        this.messagingCore = messagingCore;
        this.replies = replies;
        this.owners = owners;
    }

    String connectedChannel() {
        return messagingCore.listChannels().stream()
            .filter(c -> "WHATSAPP".equals(c.channelType()) && "CONNECTED".equals(c.status()))
            .map(MessagingCoreClient.ChannelSummary::id)
            .findFirst().orElseThrow(NoConnectedChannelException::new);
    }

    /** Queues one message; `to` is a stored contact (E.164 or "@lid") or a raw WhatsApp address. */
    public void send(String to, String text) {
        enqueue(connectedChannel(), to, text);
    }

    /** Queues the text to every owner contact; returns how many were queued. */
    public int notifyOwners(String text) {
        List<String> contacts = owners.list();
        if (contacts.isEmpty()) return 0;
        String channel = connectedChannel();
        contacts.forEach(owner -> enqueue(channel, owner, text));
        return contacts.size();
    }

    /** A reply on the same channel the inbound message came from (used for approval answers). */
    public void reply(String channelIdentityId, String to, String text) {
        enqueue(channelIdentityId, to, text);
    }

    private void enqueue(String channel, String to, String text) {
        String target = to.contains("@") ? to : ContactAddress.sendTarget(ContactAddress.canonical(to));
        replies.insert(OutboundReply.pending("prelo-out-" + UUID.randomUUID(), channel, target, text));
    }
}
