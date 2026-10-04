package dev.prelo.bridge.web;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import dev.prelo.bridge.client.MessagingCoreClient;
import dev.prelo.bridge.config.BridgeProperties;
import dev.prelo.bridge.persistence.OwnerContactStore;
import dev.prelo.bridge.service.AutoReplyGate;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.web.server.ResponseStatusException;

class AdminControllerTest {
    @Test
    void pauseImmediatelyDisablesAStaticallyEnabledBridge() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        AutoReplyGate gate = new AutoReplyGate(properties);
        AdminController controller = new AdminController(properties, gate, mock(OwnerContactStore.class), mock(MessagingCoreClient.class), mock(dev.prelo.bridge.service.OutboundMessenger.class));

        assertEquals(HttpStatus.OK, controller.pause("admin-token").getStatusCode());
        assertEquals(false, gate.isEnabled());
        assertEquals(HttpStatus.OK, controller.resume("admin-token").getStatusCode());
        assertEquals(true, gate.isEnabled());
    }

    @Test
    void listOwnerContacts_requiresTheAdminToken() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        AdminController controller = new AdminController(properties, new AutoReplyGate(properties), mock(OwnerContactStore.class), mock(MessagingCoreClient.class), mock(dev.prelo.bridge.service.OutboundMessenger.class));

        assertThrows(ResponseStatusException.class, () -> controller.listOwnerContacts("wrong-token"));
        assertThrows(ResponseStatusException.class, () -> controller.listOwnerContacts(null));
    }

    @Test
    void replaceOwnerContacts_rejectsANonE164Number() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        AdminController controller = new AdminController(properties, new AutoReplyGate(properties), mock(OwnerContactStore.class), mock(MessagingCoreClient.class), mock(dev.prelo.bridge.service.OutboundMessenger.class));

        var ex = assertThrows(ResponseStatusException.class,
            () -> controller.replaceOwnerContacts("admin-token", new AdminController.OwnerContactsRequest(List.of("41984450529"))));
        assertEquals(HttpStatus.UNPROCESSABLE_ENTITY, ex.getStatusCode());
    }

    @Test
    void replaceOwnerContacts_acceptsValidE164NumbersAndPersistsThem() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        OwnerContactStore store = mock(OwnerContactStore.class);
        when(store.list()).thenReturn(List.of("+5541984450529", "+5541996635461"));
        AdminController controller = new AdminController(properties, new AutoReplyGate(properties), store, mock(MessagingCoreClient.class), mock(dev.prelo.bridge.service.OutboundMessenger.class));

        var response = controller.replaceOwnerContacts("admin-token",
            new AdminController.OwnerContactsRequest(List.of("+5541984450529", "+5541996635461")));

        verify(store).replaceAll(List.of("+5541984450529", "+5541996635461"), "dashboard");
        assertEquals(List.of("+5541984450529", "+5541996635461"), response.getBody().contacts());
    }

    @Test
    void channelStatus_returnsOnlyWhatsAppChannels() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        MessagingCoreClient messagingCore = mock(MessagingCoreClient.class);
        when(messagingCore.listChannels()).thenReturn(List.of(
            new MessagingCoreClient.ChannelSummary("chan-1", "WHATSAPP", "CONNECTED", "554184450529:9@s.whatsapp.net", "2026-09-29T18:43:27Z"),
            new MessagingCoreClient.ChannelSummary("chan-2", "TELEGRAM", "CONNECTED", "@somebot", "2026-09-29T18:43:27Z")));
        AdminController controller = new AdminController(properties, new AutoReplyGate(properties), mock(OwnerContactStore.class), messagingCore, mock(dev.prelo.bridge.service.OutboundMessenger.class));

        var response = controller.channelStatus("admin-token");

        assertEquals(1, response.getBody().size());
        assertEquals("chan-1", response.getBody().get(0).id());
    }

    @Test
    void channelStatus_requiresTheAdminToken() {
        BridgeProperties properties = new BridgeProperties("", "", "", "", "", true, "admin-token", List.of());
        AdminController controller = new AdminController(properties, new AutoReplyGate(properties), mock(OwnerContactStore.class), mock(MessagingCoreClient.class), mock(dev.prelo.bridge.service.OutboundMessenger.class));

        assertThrows(ResponseStatusException.class, () -> controller.channelStatus("wrong-token"));
    }
}
