package dev.prelo.bridge.contact;

import static org.junit.jupiter.api.Assertions.*;

import java.util.Set;
import org.junit.jupiter.api.Test;

class ContactAddressTest {
    @Test void canonicalFormsMatchPreloCore() {
        assertEquals("+5541984450529", ContactAddress.canonical("5541984450529@s.whatsapp.net"));
        assertEquals("+5541984450529", ContactAddress.canonical("5541984450529:12@s.whatsapp.net"));
        assertEquals("+5541984450529", ContactAddress.canonical("+55 (41) 98445-0529"));
        assertEquals("+5541984450529", ContactAddress.canonical("(41) 98445-0529"));
        assertEquals("26668123456789@lid", ContactAddress.canonical("26668123456789@LID"));
    }

    @Test void brazilianNinthDigitVariantsMatchEachOther() {
        assertEquals(Set.of("+5541984450529", "+554184450529"), ContactAddress.matchKeys("554184450529@s.whatsapp.net"));
        assertEquals(Set.of("+554133334444"), ContactAddress.matchKeys("+554133334444"));
        assertEquals("5541984450529", ContactAddress.sendTarget("+5541984450529"));
        assertEquals("26668123456789@lid", ContactAddress.sendTarget("26668123456789@lid"));
    }
}
