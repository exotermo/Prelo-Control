package dev.prelo.bridge.web;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

// Matches the contract-v2 "MESSAGE_RECEIVED" envelope messaging-core's JdbcCallbackEventPublisher
// sends once a callback subscription's contractVersion is >= 2 (see that repo's
// JdbcCallbackEventPublisher.envelope) — nested, unlike the old flat "inbound_message" shape this
// used to parse. The accessors below keep the flat names InboundMessageHandler already expects.
@JsonIgnoreProperties(ignoreUnknown = true)
public record InboundMessageEvent(Channel channel, Contact contact, Message message) {
    public String channelIdentityId() { return channel == null ? null : channel.id(); }
    public String messageId() { return message == null ? null : message.id(); }
    public String from() { return contact == null ? null : contact.externalId(); }
    public String text() { return message == null ? null : message.text(); }

    public record Channel(String id, String type) { }
    public record Contact(String id, String externalId) { }
    public record Message(String id, String text, String receivedAt) { }
}
