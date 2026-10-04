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
        when(prelo.run(anyString(), anyString(), any(), any())).thenReturn(new PreloCoreClient.ExecutionResult("task-42", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.isOwner("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts, events, replies, mock(OutboundMessenger.class));
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
            verify(prelo).run(eq("hello"), eq("general"), any(), any());
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
        when(prelo.run(anyString(), anyString(), any(), any())).thenReturn(new PreloCoreClient.ExecutionResult("task-1", "COMPLETED", "reply", null));
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.isOwner("+5511999999999")).thenReturn(true);
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts, events, replies, mock(OutboundMessenger.class));

        handler.handle(new InboundMessageEvent(new InboundMessageEvent.Channel("channel-1", "WHATSAPP"),
            new InboundMessageEvent.Contact("contact-1", "+5511000000000"), new InboundMessageEvent.Message("msg", "hello", "now")));

        verify(prelo).run(eq("hello"), eq("customer"), any(), any());
    }

    // --- Fase T: the owner's WhatsApp answer to an approval request ---

    private InboundMessageEvent message(String from, String text) {
        return new InboundMessageEvent(new InboundMessageEvent.Channel("channel-1", "WHATSAPP"),
            new InboundMessageEvent.Contact("contact-1", from), new InboundMessageEvent.Message("msg", text, "now"));
    }

    @Test
    void anOwnersAnswerDecidesTheApprovalAndNeverBecomesATask() {
        PreloCoreClient prelo = mock(PreloCoreClient.class);
        OutboundMessenger messenger = mock(OutboundMessenger.class);
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        when(ownerContacts.isOwner("5541984450529@s.whatsapp.net")).thenReturn(true);
        when(prelo.decideApproval("K7Q2", false, "whatsapp:+5541984450529")).thenReturn(PreloCoreClient.DecisionOutcome.DENIED);
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts,
            mock(InboundEventStore.class), mock(OutboundReplyStore.class), messenger);

        handler.handle(message("5541984450529@s.whatsapp.net", "  NÃO k7q2 "));

        verify(prelo).decideApproval("K7Q2", false, "whatsapp:+5541984450529");
        verify(prelo, never()).run(anyString(), anyString(), any(), any());
        verify(messenger).reply(eq("channel-1"), eq("5541984450529@s.whatsapp.net"), contains("Negado"));
    }

    @Test
    void theSameWordsFromSomeoneElseAreJustAMessage() {
        PreloCoreClient prelo = mock(PreloCoreClient.class);
        when(prelo.run(anyString(), anyString(), any(), any())).thenReturn(new PreloCoreClient.ExecutionResult("t", "COMPLETED", "ok", null));
        OwnerContactStore ownerContacts = mock(OwnerContactStore.class);
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin", List.of());
        InboundMessageHandler handler = new InboundMessageHandler(prelo, new AutoReplyGate(properties), ownerContacts,
            mock(InboundEventStore.class), mock(OutboundReplyStore.class), mock(OutboundMessenger.class));

        handler.handle(message("5511000000000@s.whatsapp.net", "SIM K7Q2"));

        verify(prelo, never()).decideApproval(anyString(), anyBoolean(), anyString());
        verify(prelo).run(eq("SIM K7Q2"), eq("customer"), any(), any());
    }

    @Test
    void approvalAnswerPatterns() {
        for (String yes : List.of("SIM K7Q2", "sim k7q2", "Aprovar K7Q2!", "ok K7Q2", "s K7Q2")) {
            assertTrue(InboundMessageHandler.APPROVAL_ANSWER.matcher(yes).matches(), yes);
        }
        for (String no : List.of("Não K7Q2", "NÃO K7Q2", "nao k7q2", "negar K7Q2")) {
            assertTrue(InboundMessageHandler.APPROVAL_ANSWER.matcher(no).matches(), no);
        }
        for (String other : List.of("sim", "sim, pode fazer K7Q2 amanhã", "K7Q2", "sim K7Q2 e mais")) {
            assertTrue(!InboundMessageHandler.APPROVAL_ANSWER.matcher(other).matches(), other);
        }
    }
}
