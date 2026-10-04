package dev.prelo.bridge.web;

import dev.prelo.bridge.client.MessagingCoreClient;
import dev.prelo.bridge.config.BridgeProperties;
import dev.prelo.bridge.persistence.OwnerContactStore;
import dev.prelo.bridge.service.AutoReplyGate;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.List;
import java.util.Map;
import java.util.regex.Pattern;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.server.ResponseStatusException;

@RestController
public class AdminController {
    // E.164: a leading "+", then 1-15 digits, first digit non-zero.
    private static final Pattern E164 = Pattern.compile("^\\+[1-9]\\d{1,14}$");

    private final BridgeProperties properties;
    private final AutoReplyGate gate;
    private final OwnerContactStore ownerContacts;
    private final MessagingCoreClient messagingCore;
    private final dev.prelo.bridge.service.OutboundMessenger messenger;

    public AdminController(BridgeProperties properties, AutoReplyGate gate, OwnerContactStore ownerContacts, MessagingCoreClient messagingCore,
                           dev.prelo.bridge.service.OutboundMessenger messenger) {
        this.properties = properties;
        this.gate = gate;
        this.ownerContacts = ownerContacts;
        this.messagingCore = messagingCore;
        this.messenger = messenger;
    }

    private static final int MAX_TEXT = 4000;

    // Fase T: an agent's message to a client, sent by prelo-core only after the owner approved it.
    @PostMapping("/admin/outbound")
    public ResponseEntity<Map<String, Object>> outbound(@RequestHeader(value = "X-Admin-Token", required = false) String token,
                                                        @RequestBody OutboundRequest request) {
        requireAdminToken(token);
        if (request.to() == null || request.to().isBlank() || request.text() == null || request.text().isBlank() || request.text().length() > MAX_TEXT) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "to and text (up to 4000 characters) are required");
        }
        sendOr503(() -> messenger.send(request.to().trim(), request.text().trim()));
        return ResponseEntity.ok(Map.of("queued", 1));
    }

    // Fase T: approval requests (and other notices) to every owner contact.
    @PostMapping("/admin/owner-notifications")
    public ResponseEntity<Map<String, Object>> notifyOwners(@RequestHeader(value = "X-Admin-Token", required = false) String token,
                                                            @RequestBody NotificationRequest request) {
        requireAdminToken(token);
        if (request.text() == null || request.text().isBlank() || request.text().length() > MAX_TEXT) {
            throw new ResponseStatusException(HttpStatus.BAD_REQUEST, "text (up to 4000 characters) is required");
        }
        int[] queued = {0};
        sendOr503(() -> queued[0] = messenger.notifyOwners(request.text().trim()));
        if (queued[0] == 0) throw new ResponseStatusException(HttpStatus.CONFLICT, "no owner contact registered");
        return ResponseEntity.ok(Map.of("queued", queued[0]));
    }

    private static void sendOr503(Runnable action) {
        try {
            action.run();
        } catch (dev.prelo.bridge.service.OutboundMessenger.NoConnectedChannelException exception) {
            throw new ResponseStatusException(HttpStatus.SERVICE_UNAVAILABLE, "WhatsApp is not connected");
        }
    }

    public record OutboundRequest(String to, String text) { }
    public record NotificationRequest(String text) { }

    // Fase H4: prelo-dashboard's Integrações page. Only WhatsApp channels are relevant today
    // (the only channel type this deployment ever registers) — filtering here keeps the response
    // small and future channel types (Telegram, Email, ...) from leaking into a page that isn't
    // built to show them yet.
    @GetMapping("/admin/channel-status")
    public ResponseEntity<List<MessagingCoreClient.ChannelSummary>> channelStatus(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        List<MessagingCoreClient.ChannelSummary> whatsapp = messagingCore.listChannels().stream()
            .filter(channel -> "WHATSAPP".equals(channel.channelType()))
            .toList();
        return ResponseEntity.ok(whatsapp);
    }

    @GetMapping("/admin/auto-reply")
    public ResponseEntity<AutoReplyStatus> autoReplyStatus(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        return ResponseEntity.ok(status());
    }

    @PostMapping("/admin/pause")
    public ResponseEntity<AutoReplyStatus> pause(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        gate.pause();
        return ResponseEntity.ok(status());
    }

    @PostMapping("/admin/resume")
    public ResponseEntity<AutoReplyStatus> resume(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        gate.resume();
        return ResponseEntity.ok(status());
    }

    // Fase G2: managed by prelo-dashboard's Configurações page (via prelo-core's authenticated
    // /api/v1/settings/owner-contacts, which proxies here with the same X-Admin-Token used for
    // pause/resume above) — no new authentication surface on the bridge itself.
    @GetMapping("/admin/owner-contacts")
    public ResponseEntity<OwnerContactsResponse> listOwnerContacts(@RequestHeader(value = "X-Admin-Token", required = false) String token) {
        requireAdminToken(token);
        return ResponseEntity.ok(new OwnerContactsResponse(ownerContacts.list()));
    }

    @PutMapping("/admin/owner-contacts")
    public ResponseEntity<OwnerContactsResponse> replaceOwnerContacts(@RequestHeader(value = "X-Admin-Token", required = false) String token,
                                                                       @RequestBody OwnerContactsRequest request) {
        requireAdminToken(token);
        List<String> contacts = request.contacts() == null ? List.of() : request.contacts();
        for (String phone : contacts) {
            if (phone == null || !E164.matcher(phone).matches()) {
                throw new ResponseStatusException(HttpStatus.UNPROCESSABLE_ENTITY, "each contact must be E.164 (e.g. +5511999999999): " + phone);
            }
        }
        ownerContacts.replaceAll(contacts, "dashboard");
        return ResponseEntity.ok(new OwnerContactsResponse(ownerContacts.list()));
    }

    public record OwnerContactsRequest(List<String> contacts) { }
    public record OwnerContactsResponse(List<String> contacts) { }

    private void requireAdminToken(String provided) {
        String expected = properties.adminToken();
        if (provided == null || expected == null || expected.isBlank()
            || !MessageDigest.isEqual(provided.getBytes(StandardCharsets.UTF_8), expected.getBytes(StandardCharsets.UTF_8))) {
            throw new org.springframework.web.server.ResponseStatusException(HttpStatus.UNAUTHORIZED, "invalid admin token");
        }
    }

    private AutoReplyStatus status() {
        return new AutoReplyStatus(properties.autoReplyEnabled(), gate.isRuntimeEnabled(), gate.isEnabled());
    }

    public record AutoReplyStatus(boolean staticEnabled, boolean runtimeEnabled, boolean effectiveEnabled) { }
}
