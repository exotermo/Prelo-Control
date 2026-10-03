package dev.hermes.bridge.persistence;

import java.time.Instant;

// A phone number (E.164) routed to the full-capability "general" Hermes agent — everyone else
// gets "customer" (see InboundMessageHandler). Managed through the hermes-dashboard
// Configurações page; BRIDGE_OWNER_CONTACTS is only a one-time seed for an empty table now.
public record OwnerContact(String phoneE164, Instant addedAt, String addedBy) {
}
