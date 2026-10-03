package br.com.exotermo.hermes.gateway.connection;

import static org.junit.jupiter.api.Assertions.*;

import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import org.junit.jupiter.api.Test;

class ConnectionKeyCipherTest {
    private static byte[] key() {
        byte[] key = new byte[32];
        new SecureRandom().nextBytes(key);
        return key;
    }

    @Test void roundTrips() {
        var cipher = new ConnectionKeyCipher(key());
        var sealed = cipher.seal("sk-ant-secret-value", "row-1:anthropic");
        assertEquals("sk-ant-secret-value", cipher.open(sealed.salt(), sealed.payload(), "row-1:anthropic"));
    }

    @Test void neverStoresThePlaintextAndUsesFreshSaltAndNonceEachTime() {
        var cipher = new ConnectionKeyCipher(key());
        var a = cipher.seal("same-key-value", "row-1:openai");
        var b = cipher.seal("same-key-value", "row-1:openai");
        assertEquals(32, a.salt().length);
        assertFalse(new String(a.payload(), StandardCharsets.ISO_8859_1).contains("same-key-value"));
        assertFalse(java.util.Arrays.equals(a.salt(), b.salt()));
        assertFalse(java.util.Arrays.equals(a.payload(), b.payload()));
    }

    @Test void refusesCiphertextMovedToAnotherRowOrProvider() {
        var cipher = new ConnectionKeyCipher(key());
        var sealed = cipher.seal("secret", "row-1:anthropic");
        assertThrows(IllegalStateException.class, () -> cipher.open(sealed.salt(), sealed.payload(), "row-2:anthropic"));
        assertThrows(IllegalStateException.class, () -> cipher.open(sealed.salt(), sealed.payload(), "row-1:openai"));
    }

    @Test void refusesTamperingAndTheWrongMasterKey() {
        var cipher = new ConnectionKeyCipher(key());
        var sealed = cipher.seal("secret", "row-1:anthropic");
        byte[] tampered = sealed.payload().clone();
        tampered[tampered.length - 1] ^= 1;
        assertThrows(IllegalStateException.class, () -> cipher.open(sealed.salt(), tampered, "row-1:anthropic"));
        assertThrows(IllegalStateException.class, () -> new ConnectionKeyCipher(key()).open(sealed.salt(), sealed.payload(), "row-1:anthropic"));
    }

    @Test void requiresA32ByteMasterKey() {
        assertThrows(IllegalArgumentException.class, () -> new ConnectionKeyCipher(new byte[16]));
    }
}
