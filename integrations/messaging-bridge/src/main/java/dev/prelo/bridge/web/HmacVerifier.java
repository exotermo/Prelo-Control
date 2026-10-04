package dev.prelo.bridge.web;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.time.Duration;
import java.time.Instant;
import java.util.HexFormat;
import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

// Verifies callback signatures using the raw request bytes. Contract v2 signs
// timestamp + "." + rawBody; v1 (body-only) is retained only for an explicit migration window.
public final class HmacVerifier {
    private static final String PREFIX = "sha256=";

    private HmacVerifier() { }

    public static boolean verify(String secret, byte[] rawBody, String signatureHeader) {
        if (secret == null || secret.isBlank() || signatureHeader == null || !signatureHeader.startsWith(PREFIX)) {
            return false;
        }
        String provided = signatureHeader.substring(PREFIX.length());
        String expected = hmacSha256Hex(secret, rawBody);
        return MessageDigest.isEqual(provided.getBytes(StandardCharsets.UTF_8), expected.getBytes(StandardCharsets.UTF_8));
    }

    public static boolean verifyV2(String secret, byte[] rawBody, String timestampHeader,
                                   String signatureHeader, Duration tolerance) {
        return verifyV2(secret, rawBody, timestampHeader, signatureHeader, tolerance, Instant.now());
    }

    static boolean verifyV2(String secret, byte[] rawBody, String timestampHeader,
                            String signatureHeader, Duration tolerance, Instant now) {
        if (secret == null || secret.isBlank() || rawBody == null || timestampHeader == null
            || signatureHeader == null || !signatureHeader.startsWith(PREFIX) || tolerance == null
            || tolerance.isNegative()) return false;
        final long timestamp;
        try {
            timestamp = Long.parseLong(timestampHeader.trim());
        } catch (NumberFormatException exception) {
            return false;
        }
        Instant signedAt;
        try {
            signedAt = Instant.ofEpochSecond(timestamp);
        } catch (RuntimeException exception) {
            return false;
        }
        if (Math.abs(Duration.between(signedAt, now).toSeconds()) > tolerance.toSeconds()) return false;
        String expected = hmacSha256Hex(secret, (timestampHeader.trim() + ".").getBytes(StandardCharsets.UTF_8), rawBody);
        String provided = signatureHeader.substring(PREFIX.length());
        return MessageDigest.isEqual(provided.getBytes(StandardCharsets.UTF_8), expected.getBytes(StandardCharsets.UTF_8));
    }

    private static String hmacSha256Hex(String secret, byte[] body) {
        return hmacSha256Hex(secret, new byte[0], body);
    }

    private static String hmacSha256Hex(String secret, byte[] prefix, byte[] body) {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            mac.update(prefix);
            return HexFormat.of().formatHex(mac.doFinal(body));
        } catch (Exception exception) {
            throw new IllegalStateException("failed to compute HMAC signature", exception);
        }
    }
}
