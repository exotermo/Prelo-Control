package dev.prelo.bridge.service;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

import ch.qos.logback.classic.Logger;
import ch.qos.logback.classic.spi.ILoggingEvent;
import ch.qos.logback.core.read.ListAppender;
import dev.prelo.bridge.client.PreloCoreClient;
import dev.prelo.bridge.config.BridgeProperties;
import dev.prelo.bridge.persistence.InboundEventStatus;
import dev.prelo.bridge.persistence.InboundEventStore;
import dev.prelo.bridge.persistence.OutboundReply;
import dev.prelo.bridge.persistence.OutboundReplyStore;
import dev.prelo.bridge.persistence.OwnerContactStore;
import dev.prelo.bridge.web.InboundMessageEvent;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.slf4j.LoggerFactory;

class InboundMessageHandlerTest {
    @Test
    void queuesAnOutboundReplyAndLogsAfterAnAutomaticReplyIsAccepted() {
        PreloCoreClient prelo = mock(PreloCoreClient.class);
        InboundEventStore events = mock(InboundEventStore.class);
        OutboundReplyStore replies = mock(OutboundReplyStore.class);
        when(prelo.run(anyString(), anyString(), any())).thenReturn(new PreloCoreClient.ExecutionResult("task-42", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.contains("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts, events, replies);
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
            verify(prelo).run(eq("hello"), eq("general"), any());
            assertTrue(appender.list.stream().map(ILoggingEvent::getFormattedMessage)
                .anyMatch(message -> message.contains("reply_queued") && message.contains("taskId=task-42") && !message.contains("responseHash=reply")));
        } finally {
            logger.detachAppender(appender);
        }
    }

    @Test
    void routesAnUnknownSenderToTheRestrictedCustomerAgent() {
        PreloCoreClient prelo = mock(PreloCoreClient.class);
        InboundEventStore events = mock(InboundEventStore.class);
        OutboundReplyStore replies = mock(OutboundReplyStore.class);
        when(prelo.run(anyString(), anyString(), any())).thenReturn(new PreloCoreClient.ExecutionResult("task-1", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.contains("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts, events, replies);

        handler.handle(new InboundMessageEvent(new InboundMessageEvent.Channel("channel-1", "WHATSAPP"),
            new InboundMessageEvent.Contact("contact-1", "+5511000000000"), new InboundMessageEvent.Message("msg", "hello", "now")));

        verify(prelo).run(eq("hello"), eq("customer"), any());
    }
}
