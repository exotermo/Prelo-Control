package dev.prelo.bridge.persistence;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Component;

// Fase G2: the source of truth for who reaches the full-capability "general" agent. Small
// enough (a handful of numbers) that "replace the whole list" is the only write operation the
// Configurações page needs — see BridgeProperties.ownerContacts's doc for why this must never
// silently grant capabilities to a WhatsApp contact just for being allowed to message at all.
@Component
public class OwnerContactStore {
    private final JdbcTemplate jdbc;

    public OwnerContactStore(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public List<String> list() {
        return jdbc.query("SELECT phone_e164 FROM owner_contacts ORDER BY added_at", (rs, rowNum) -> rs.getString("phone_e164"));
    }

    /**
     * Fase T: whether a WhatsApp sender (phone JID, typed number or "@lid" ID) is one of the owners,
     * comparing normalized numbers — the sender no longer has to match the stored text exactly.
     */
    public boolean isOwner(String sender) {
        java.util.Set<String> keys = dev.prelo.bridge.contact.ContactAddress.matchKeys(sender);
        if (keys.isEmpty()) return false;
        for (String owner : list()) {
            if (keys.contains(dev.prelo.bridge.contact.ContactAddress.canonical(owner))) return true;
        }
        return false;
    }

    public boolean contains(String phoneE164) {
        Integer count = jdbc.queryForObject("SELECT count(*) FROM owner_contacts WHERE phone_e164 = ?", Integer.class, phoneE164);
        return count != null && count > 0;
    }

    public boolean isEmpty() {
        Integer count = jdbc.queryForObject("SELECT count(*) FROM owner_contacts", Integer.class);
        return count == null || count == 0;
    }

    // Replaces the whole list atomically — the Configurações page always submits the full set
    // it wants, never an incremental add/remove, so there is no lost-update race to guard
    // against between two admins editing at once (last write wins, same as any settings form).
    public void replaceAll(List<String> phoneNumbers, String addedBy) {
        jdbc.update("DELETE FROM owner_contacts");
        Instant now = Instant.now();
        for (String phone : phoneNumbers) {
            jdbc.update("INSERT INTO owner_contacts (phone_e164, added_at, added_by) VALUES (?,?,?)", phone, Timestamp.from(now), addedBy);
        }
    }

    // Seeds from BRIDGE_OWNER_CONTACTS only once, on boot, only while the table is still empty —
    // after that the table is the only source of truth and the env var is inert. Silently
    // ignores a number already present (defensive; the table is empty when this runs).
    public void seedIfEmpty(List<String> phoneNumbers) {
        if (phoneNumbers.isEmpty() || !isEmpty()) return;
        for (String phone : phoneNumbers) {
            try {
                jdbc.update("INSERT INTO owner_contacts (phone_e164, added_at, added_by) VALUES (?,?,?)", phone, Timestamp.from(Instant.now()), "seed:BRIDGE_OWNER_CONTACTS");
            } catch (DuplicateKeyException ignored) {
                // defensive only; a fresh/empty table never actually hits this
            }
        }
    }
}
