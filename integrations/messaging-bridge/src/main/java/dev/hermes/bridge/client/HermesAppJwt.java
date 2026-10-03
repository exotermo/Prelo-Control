package dev.hermes.bridge.client;

import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Base64;
import java.util.List;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

// Mints the HS256 JWT HermesAppClient presents to hermes-go. hermes-go's JWTAuthMiddleware trusts
// anything signed with the same shared secret (see BridgeProperties.hermesGoApiJwtSecret) and
// reads scope/token_use/tenant_id straight off the claims — no network round trip to an issuer is
// needed, the same way hermes-go self-signs its own dashboard sessions.
final class HermesAppJwt {
    private static final Base64.Encoder BASE64 = Base64.getUrlEncoder().withoutPadding();

    private HermesAppJwt() { }

    static String mint(String secret, String issuer, String audience, List<String> scopes, java.time.Duration ttl) {
        Instant now = Instant.now();
        String header = BASE64.encodeToString("{\"alg\":\"HS256\",\"typ\":\"JWT\"}".getBytes(StandardCharsets.UTF_8));
        String scopeJson = scopes.stream().map(s -> "\"" + s + "\"").reduce((a, b) -> a + "," + b).orElse("");
        String payload = "{"
            + "\"sub\":\"hermes-messaging-bridge\","
            + "\"token_use\":\"technical\","
            + "\"tenant_id\":\"00000000-0000-0000-0000-000000000000\","
            + "\"scope\":[" + scopeJson + "],"
            + "\"iss\":\"" + issuer + "\","
            + "\"aud\":\"" + audience + "\","
            + "\"iat\":" + now.getEpochSecond() + ","
            + "\"exp\":" + now.plus(ttl).getEpochSecond()
            + "}";
        String encodedPayload = BASE64.encodeToString(payload.getBytes(StandardCharsets.UTF_8));
        String signingInput = header + "." + encodedPayload;
        String signature = BASE64.encodeToString(hmacSha256(secret, signingInput));
        return signingInput + "." + signature;
    }

    private static byte[] hmacSha256(String secret, String data) {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            return mac.doFinal(data.getBytes(StandardCharsets.UTF_8));
        } catch (Exception exception) {
            throw new IllegalStateException("failed to sign hermes-go token", exception);
        }
    }
}
