package dev.prelo.bridge.config;

import java.util.List;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.bind.ConstructorBinding;

@ConfigurationProperties("bridge")
public record BridgeProperties(
    String messagingCoreUrl,
    String messagingCoreClientId,
    String messagingCoreClientSecret,
    String callbackSigningSecret,
    String preloCoreUrl,
    boolean autoReplyEnabled,
    String adminToken,
    // E.164 numbers routed to the "general" Prelo agent (full tool capabilities); every other
    // sender gets "customer" (no capabilities) — Fase F point 4, a WhatsApp contact must never
    // inherit the owner's agent capabilities just by being on the allowlist.
    List<String> ownerContacts,
    int callbackReplayToleranceSeconds,
    boolean allowLegacyCallbackSignatures,
    // Self-signs the token PreloCoreClient presents to prelo-core — must be the exact same secret
    // prelo-core validates against (PRELO_API_JWT_SECRET there). Minting locally, instead of
    // reusing the client_credentials token messagingCoreClient already has for messaging-core's
    // OWN API, is deliberate: that token's scopes (messages:send/channels:manage/callbacks:manage)
    // are messaging-core's, not prelo-core's, and prelo-core rejects it with 403 — found 2026-10-01
    // the first time a real inbound WhatsApp message exercised this path end to end.
    String preloApiJwtSecret,
    String preloApiJwtIssuer,
    String preloApiJwtAudience
) {
    // Explicit, because a second (compatibility) constructor below would otherwise leave Spring
    // unable to infer which one binds bridge.* properties — without this it fails at startup
    // with "No default constructor found" instead of picking the canonical one.
    @ConstructorBinding
    public BridgeProperties {
        if (ownerContacts == null) ownerContacts = List.of();
        if (callbackReplayToleranceSeconds <= 0) callbackReplayToleranceSeconds = 300;
        if (preloApiJwtIssuer == null || preloApiJwtIssuer.isBlank()) preloApiJwtIssuer = "messaging-core";
        if (preloApiJwtAudience == null || preloApiJwtAudience.isBlank()) preloApiJwtAudience = "messaging-core";
    }

    // Compatibility constructor for callers/tests created before callback contract v2.
    public BridgeProperties(String messagingCoreUrl, String messagingCoreClientId, String messagingCoreClientSecret,
                            String callbackSigningSecret, String preloCoreUrl, boolean autoReplyEnabled,
                            String adminToken, List<String> ownerContacts) {
        this(messagingCoreUrl, messagingCoreClientId, messagingCoreClientSecret, callbackSigningSecret,
            preloCoreUrl, autoReplyEnabled, adminToken, ownerContacts, 300, true, null, "messaging-core", "messaging-core");
    }

    @Override
    public String toString() {
        return "BridgeProperties[messagingCoreUrl=" + messagingCoreUrl + ", messagingCoreClientId=" + messagingCoreClientId
            + ", messagingCoreClientSecret=<redacted>, callbackSigningSecret=<redacted>, preloCoreUrl=" + preloCoreUrl
            + ", autoReplyEnabled=" + autoReplyEnabled + ", adminToken=<redacted>, ownerContacts=" + ownerContacts.size() + " configured]";
    }
}
