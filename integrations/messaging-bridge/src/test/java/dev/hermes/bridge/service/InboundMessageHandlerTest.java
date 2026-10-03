package dev.hermes.bridge.service;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import dev.hermes.bridge.client.HermesAppClient;
import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.persistence.InboundEventStatus;
import dev.hermes.bridge.persistence.InboundEventStore;
import dev.hermes.bridge.persistence.OutboundReply;
import dev.hermes.bridge.persistence.OutboundReplyStore;
import dev.hermes.bridge.persistence.OwnerContactStore;
import dev.hermes.bridge.web.InboundMessageEvent;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.slf4j.LoggerFactory;

class InboundMessageHandlerTest {
    @Test
    void queuesAnOutboundReplyAndLogsAfterAnAutomaticReplyIsAccepted() {
        HermesAppClient hermes = mock(HermesAppClient.class);
        InboundEventStore events = mock(InboundEventStore.class);
        OutboundReplyStore replies = mock(OutboundReplyStore.class);
        when(hermes.run(anyString(), anyString(), any())).thenReturn(new HermesAppClient.ExecutionResult("task-42", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.contains("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(hermes, new AutoReplyGate(properties), ownerContacts, events, replies);
        Logger logger = (Logger) LoggerFactory.getLogger(InboundMessageHandler.class);
        ListAppender<ILoggingEvent> appender = new ListAppender<>();
        appender.start();
        logger.addAppender(appender);
        try {
            handler.handle(new InboundMessageEvent(new InboundMessageEvent.Channel("channel-1", "WHATSAPP"),
                new InboundMessageEvent.Contact("contact-1", "+5511999999999"), new InboundMessageEvent.Message("msg", "hello", "now")));

            ArgumentCaptor<OutboundReply> captor = ArgumentCaptor.forClass(OutboundReply.class);
            verify(replies).insert(captor.capture());
            assertEquals("msg", captor.getValue().messageId());
            assertEquals("channel-1", captor.getValue().channelIdentityId());
            assertEquals("+5511999999999", captor.getValue().toAddress());
            assertEquals("reply", captor.getValue().text());
            verify(events).markStatus("msg", InboundEventStatus.DONE);
            // The sender is a configured owner, so the "general" agent (full capabilities) is used.
            verify(hermes).run(eq("hello"), eq("general"), any());
            assertTrue(appender.list.stream().map(ILoggingEvent::getFormattedMessage)
                .anyMatch(message -> message.contains("reply_queued") && message.contains("taskId=task-42") && !message.contains("responseHash=reply")));
        } finally {
            logger.detachAppender(appender);
        }
    }

    @Test
    void routesAnUnknownSenderToTheRestrictedCustomerAgent() {
        HermesAppClient hermes = mock(HermesAppClient.class);
        InboundEventStore events = mock(InboundEventStore.class);
        OutboundReplyStore replies = mock(OutboundReplyStore.class);
        when(hermes.run(anyString(), anyString(), any())).thenReturn(new HermesAppClient.ExecutionResult("task-1", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.contains("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(hermes, new AutoReplyGate(properties), ownerContacts, events, replies);

        handler.handle(new InboundMessageEvent(new InboundMessageEvent.Channel("channel-1", "WHATSAPP"),
            new InboundMessageEvent.Contact("contact-1", "+5511000000000"), new InboundMessageEvent.Message("msg", "hello", "now")));

        verify(hermes).run(eq("hello"), eq("customer"), any());
    }
}
