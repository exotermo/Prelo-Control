package dev.hermes.bridge.config;

import java.util.List;
import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties("bridge")
public record BridgeProperties(
    String messagingCoreUrl,
    String messagingCoreClientId,
    String messagingCoreClientSecret,
    String callbackSigningSecret,
    String hermesAppUrl,
    boolean autoReplyEnabled,
    String adminToken,
    // E.164 numbers routed to the "general" Hermes agent (full tool capabilities); every other
    // sender gets "customer" (no capabilities) — Fase F point 4, a WhatsApp contact must never
    // inherit the owner's agent capabilities just by being on the allowlist.
    List<String> ownerContacts,
    int callbackReplayToleranceSeconds,
    boolean allowLegacyCallbackSignatures
) {
    public BridgeProperties {
        if (ownerContacts == null) ownerContacts = List.of();
        if (callbackReplayToleranceSeconds <= 0) callbackReplayToleranceSeconds = 300;
    }

    // Compatibility constructor for callers/tests created before callback contract v2.
    public BridgeProperties(String messagingCoreUrl, String messagingCoreClientId, String messagingCoreClientSecret,
                            String callbackSigningSecret, String hermesAppUrl, boolean autoReplyEnabled,
                            String adminToken, List<String> ownerContacts) {
        this(messagingCoreUrl, messagingCoreClientId, messagingCoreClientSecret, callbackSigningSecret,
            hermesAppUrl, autoReplyEnabled, adminToken, ownerContacts, 300, true);
    }

    @Override
    public String toString() {
        return "BridgeProperties[messagingCoreUrl=" + messagingCoreUrl + ", messagingCoreClientId=" + messagingCoreClientId
            + ", messagingCoreClientSecret=<redacted>, callbackSigningSecret=<redacted>, hermesAppUrl=" + hermesAppUrl
            + ", autoReplyEnabled=" + autoReplyEnabled + ", adminToken=<redacted>, ownerContacts=" + ownerContacts.size() + " configured]";
    }
}
