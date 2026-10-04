package br.com.exotermo.prelo.gateway.connection;

import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.SecureRandom;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.Mac;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/**
 * Envelope protection for stored provider API keys (Fase M).
 *
 * <p>Each sealed key gets its own data key: HKDF-SHA256(masterKey, salt) with a fresh 32-byte random
 * salt per write, so no two records ever share a key even when the plaintext repeats. The data key
 * encrypts with AES-256-GCM under a fresh 12-byte nonce, and the associated data binds the
 * ciphertext to its row (connection id + provider): copying a ciphertext into another row, or
 * changing a row's provider, makes decryption fail instead of silently handing the key to the
 * wrong place. The master key lives outside the database (a Docker secret), so a database dump
 * alone opens nothing.
 */
public final class ConnectionKeyCipher {
    private static final int KEY_BYTES = 32;
    private static final int SALT_BYTES = 32;
    private static final int NONCE_BYTES = 12;
    private static final int TAG_BITS = 128;
    private static final byte[] HKDF_INFO = "prelo-gateway/provider-connection/v1".getBytes(StandardCharsets.UTF_8);

    private final byte[] masterKey;
    private final SecureRandom random = new SecureRandom();

    public record Sealed(byte[] salt, byte[] payload) { }

    public ConnectionKeyCipher(byte[] masterKey) {
        if (masterKey == null || masterKey.length != KEY_BYTES) {
            throw new IllegalArgumentException("connection master key must be exactly 32 bytes");
        }
        this.masterKey = masterKey.clone();
    }

    public Sealed seal(String plaintext, String associatedData) {
        byte[] salt = new byte[SALT_BYTES];
        byte[] nonce = new byte[NONCE_BYTES];
        random.nextBytes(salt);
        random.nextBytes(nonce);
        byte[] dataKey = deriveKey(salt);
        try {
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(dataKey, "AES"), new GCMParameterSpec(TAG_BITS, nonce));
            cipher.updateAAD(associatedData.getBytes(StandardCharsets.UTF_8));
            byte[] ciphertext = cipher.doFinal(plaintext.getBytes(StandardCharsets.UTF_8));
            return new Sealed(salt, ByteBuffer.allocate(NONCE_BYTES + ciphertext.length).put(nonce).put(ciphertext).array());
        } catch (GeneralSecurityException exception) {
            throw new IllegalStateException("could not seal provider key");
        } finally {
            Arrays.fill(dataKey, (byte) 0);
        }
    }

    public String open(byte[] salt, byte[] payload, String associatedData) {
        if (salt == null || payload == null || payload.length <= NONCE_BYTES) {
            throw new IllegalStateException("stored provider key is malformed");
        }
        byte[] dataKey = deriveKey(salt);
        try {
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, new SecretKeySpec(dataKey, "AES"), new GCMParameterSpec(TAG_BITS, payload, 0, NONCE_BYTES));
            cipher.updateAAD(associatedData.getBytes(StandardCharsets.UTF_8));
            byte[] plaintext = cipher.doFinal(payload, NONCE_BYTES, payload.length - NONCE_BYTES);
            return new String(plaintext, StandardCharsets.UTF_8);
        } catch (GeneralSecurityException exception) {
            // Wrong master key, tampered ciphertext, or ciphertext moved to another row.
            throw new IllegalStateException("stored provider key could not be decrypted");
        } finally {
            Arrays.fill(dataKey, (byte) 0);
        }
    }

    // RFC 5869 HKDF-SHA256 with a single expand block (L = 32 = HashLen). JDK 21 has no KDF API yet.
    private byte[] deriveKey(byte[] salt) {
        try {
            Mac extract = Mac.getInstance("HmacSHA256");
            extract.init(new SecretKeySpec(salt, "HmacSHA256"));
            byte[] prk = extract.doFinal(masterKey);
            Mac expand = Mac.getInstance("HmacSHA256");
            expand.init(new SecretKeySpec(prk, "HmacSHA256"));
            expand.update(HKDF_INFO);
            expand.update((byte) 0x01);
            byte[] okm = expand.doFinal();
            Arrays.fill(prk, (byte) 0);
            return okm;
        } catch (GeneralSecurityException exception) {
            throw new IllegalStateException("could not derive data key");
        }
    }
}
