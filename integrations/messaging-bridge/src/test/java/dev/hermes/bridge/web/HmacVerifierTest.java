package dev.hermes.bridge.web;

import static org.junit.jupiter.api.Assertions.*;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;

class HmacVerifierTest {
    private static final String SECRET = "test-secret";
    private static final byte[] BODY = "{\"hello\":\"world\"}".getBytes(StandardCharsets.UTF_8);

    @Test
    void acceptsAValidSignature() throws Exception {
        String signature = "sha256=" + hmacHex(SECRET, BODY);
        assertTrue(HmacVerifier.verify(SECRET, BODY, signature));
    }

    @Test
    void rejectsAnInvalidSignature() {
        assertFalse(HmacVerifier.verify(SECRET, BODY, "sha256=deadbeef"));
    }

    @Test
    void rejectsAMissingHeader() {
        assertFalse(HmacVerifier.verify(SECRET, BODY, null));
    }

    @Test
    void rejectsASignatureComputedWithTheWrongSecret() throws Exception {
        String signature = "sha256=" + hmacHex("wrong-secret", BODY);
        assertFalse(HmacVerifier.verify(SECRET, BODY, signature));
    }

    @Test
    void acceptsV2TimestampedSignature() throws Exception {
        String timestamp = "1727000000";
        String signature = "sha256=" + hmacHex(SECRET, (timestamp + "." + new String(BODY, StandardCharsets.UTF_8)).getBytes(StandardCharsets.UTF_8));
        assertTrue(HmacVerifier.verifyV2(SECRET, BODY, timestamp, signature, Duration.ofDays(365), Instant.ofEpochSecond(1727000000)));
    }

    @Test
    void rejectsV2ReplayOutsideTimestampWindow() throws Exception {
        String timestamp = "1727000000";
        String signature = "sha256=" + hmacHex(SECRET, (timestamp + "." + new String(BODY, StandardCharsets.UTF_8)).getBytes(StandardCharsets.UTF_8));
        assertFalse(HmacVerifier.verifyV2(SECRET, BODY, timestamp, signature, Duration.ofMinutes(5), Instant.ofEpochSecond(1727001000)));
    }

    private static String hmacHex(String secret, byte[] body) throws Exception {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
        return java.util.HexFormat.of().formatHex(mac.doFinal(body));
    }
}
