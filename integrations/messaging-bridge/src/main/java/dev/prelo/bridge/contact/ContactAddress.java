package dev.prelo.bridge.contact;

import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.Set;

/**
 * Fase T: one way to compare WhatsApp senders with stored contacts — same rule as prelo-core's
 * domain.CanonicalWhatsAppAddress/ContactMatchKeys. A phone JID ("5541…@s.whatsapp.net", with or
 * without a device suffix) or a typed number becomes E.164 ("+5541…"); a WhatsApp ID ("…@lid")
 * stays as it is. Brazilian mobiles match with and without the extra 9.
 */
public final class ContactAddress {
    private ContactAddress() { }

    public static String canonical(String raw) {
        if (raw == null) return null;
        String v = raw.trim().toLowerCase(Locale.ROOT);
        int colon = v.indexOf(':');
        int at = v.indexOf('@');
        if (colon > 0 && at > colon) v = v.substring(0, colon) + v.substring(at);
        if (v.endsWith("@lid")) return v;
        if (v.endsWith("@s.whatsapp.net")) v = v.substring(0, v.indexOf('@'));
        else if (v.endsWith("@c.us")) v = v.substring(0, v.indexOf('@'));
        String digits = v.replaceAll("[^0-9]", "");
        if (digits.isEmpty()) return null;
        if (!v.startsWith("+") && (digits.length() == 10 || digits.length() == 11)) digits = "55" + digits;
        return "+" + digits;
    }

    public static Set<String> matchKeys(String raw) {
        Set<String> keys = new LinkedHashSet<>();
        String c = canonical(raw);
        if (c == null) return keys;
        keys.add(c);
        if (c.startsWith("+55")) {
            String d = c.substring(3);
            if (d.length() == 11 && d.charAt(2) == '9') keys.add("+55" + d.substring(0, 2) + d.substring(3));
            else if (d.length() == 10 && d.charAt(2) >= '6') keys.add("+55" + d.substring(0, 2) + "9" + d.substring(2));
        }
        return keys;
    }

    /** What the WhatsApp sidecar addresses: digits for a phone ("5541…"), the ID as is for "@lid". */
    public static String sendTarget(String stored) {
        if (stored == null) return null;
        return stored.startsWith("+") ? stored.substring(1) : stored;
    }
}
