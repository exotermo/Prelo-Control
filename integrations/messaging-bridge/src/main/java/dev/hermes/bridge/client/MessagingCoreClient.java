package dev.hermes.bridge.client;

import dev.hermes.bridge.config.BridgeProperties;
import java.time.Instant;
import java.util.Arrays;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.concurrent.locks.ReentrantLock;
import java.nio.charset.StandardCharsets;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClient;

// Talks to messaging-core's public API: mints a client_credentials token (cached until it's
// close to its 900s TTL) and sends the reply via POST /api/v1/messages.
@Component
public class MessagingCoreClient {
    private static final Logger log = LoggerFactory.getLogger(MessagingCoreClient.class);
    private static final long TOKEN_TTL_SAFETY_MARGIN_SECONDS = 30;

    private final RestClient client;
    private final BridgeProperties properties;
    private final ReentrantLock tokenLock = new ReentrantLock();

    private volatile String cachedToken;
    private volatile Instant cachedTokenExpiresAt = Instant.EPOCH;

    public MessagingCoreClient(BridgeProperties properties) {
        this.properties = properties;
        this.client = RestClient.builder().baseUrl(properties.messagingCoreUrl()).build();
    }

    // clientMessageId is the bridge's own inbound messageId, reused as messaging-core's
    // idempotency key for the send (see that repo's SendMessageUseCase) — a retried call after a
    // crash between "accepted" and "outbound_replies row marked SENT" resolves to the same
    // OutboundMessage instead of sending a duplicate WhatsApp message.
    public void sendMessage(String channelIdentityId, String to, String text, String clientMessageId) {
        String token = accessToken();
        client.post().uri("/api/v1/messages")
            .header(HttpHeaders.AUTHORIZATION, "Bearer " + token)
            .contentType(MediaType.APPLICATION_JSON)
            .body(Map.of("channelIdentityId", channelIdentityId, "to", to, "text", text, "clientMessageId", clientMessageId))
            .retrieve()
            .toBodilessEntity();
    }

    /** Returns the cached integration token for other first-party services in the same trust
     * boundary. The token is never logged or returned to an HTTP caller. */
    public String accessToken() { return token(); }

    // Fase H4: hermes-dashboard's Integrações page needs to show WhatsApp connection status
    // without anyone opening a shell/psql — see AdminController.channelStatus(). Reuses the same
    // client_credentials token already minted for sendMessage; no new auth surface.
    public List<ChannelSummary> listChannels() {
        String token = accessToken();
        ChannelSummary[] response = client.get().uri("/api/v1/channels")
            .header(HttpHeaders.AUTHORIZATION, "Bearer " + token)
            .retrieve()
            .body(ChannelSummary[].class);
        return response == null ? List.of() : Arrays.asList(response);
    }

    public record ChannelSummary(String id, String channelType, String status, String externalRef, String createdAt) { }

    private String token() {
        if (cachedToken != null && Instant.now().isBefore(cachedTokenExpiresAt)) {
            return cachedToken;
        }
        tokenLock.lock();
        try {
            if (cachedToken != null && Instant.now().isBefore(cachedTokenExpiresAt)) {
                return cachedToken;
            }
            String basic = Base64.getEncoder().encodeToString(
                (properties.messagingCoreClientId() + ":" + properties.messagingCoreClientSecret())
                    .getBytes(StandardCharsets.UTF_8));

            Map<?, ?> response = client.post().uri("/oauth/token")
                .header(HttpHeaders.AUTHORIZATION, "Basic " + basic)
                .contentType(MediaType.APPLICATION_FORM_URLENCODED)
                .body("grant_type=client_credentials")
                .retrieve()
                .body(Map.class);

            cachedToken = (String) response.get("access_token");
            long expiresIn = ((Number) response.get("expires_in")).longValue();
            cachedTokenExpiresAt = Instant.now().plusSeconds(Math.max(1, expiresIn - TOKEN_TTL_SAFETY_MARGIN_SECONDS));
            log.debug("minted a fresh messaging-core token, valid for {}s", expiresIn);
            return cachedToken;
        } finally {
            tokenLock.unlock();
        }
    }
}
