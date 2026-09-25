package dev.hermes.bridge.web;

// Matches the payload messaging-core's JdbcCallbackEventPublisher serializes for the
// "inbound_message" event type — see that repo's InboundMessageEvent record.
public record InboundMessageEvent(
    String eventType,
    String channelIdentityId,
    String conversationId,
    String messageId,
    String externalMessageId,
    String from,
    String text,
    String receivedAt
) { }
