package br.com.exotermo.prelo.gateway.connection;

import jakarta.persistence.*;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "provider_connections")
public class ProviderConnection {
    public static final String SCOPE_INSTANCE = "INSTANCE";
    public static final String SCOPE_PROJECT = "PROJECT";

    @Id private UUID id;
    private String scope;
    private UUID projectId;
    private String provider;
    private String baseUrl;
    private String model;
    private boolean active;
    private byte[] keySalt;
    private byte[] encryptedKey;
    private String keyLast4;
    private Instant lastTestAt;
    private Boolean lastTestOk;
    private Long lastTestLatencyMs;
    private String lastTestError;
    private Instant createdAt;
    private Instant updatedAt;
    @Version private long version;

    protected ProviderConnection() { }

    ProviderConnection(String scope, UUID projectId) {
        this.id = UUID.randomUUID();
        this.scope = scope;
        this.projectId = projectId;
        this.active = true;
        this.createdAt = Instant.now();
        this.updatedAt = this.createdAt;
    }

    /** Binds a sealed key to this exact row and provider (see ConnectionKeyCipher). */
    String associatedData() { return id + ":" + provider; }

    void configure(String provider, String baseUrl, String model) {
        this.provider = provider;
        this.baseUrl = baseUrl;
        this.model = model;
        this.updatedAt = Instant.now();
    }

    void storeKey(ConnectionKeyCipher.Sealed sealed, String last4) {
        this.keySalt = sealed.salt();
        this.encryptedKey = sealed.payload();
        this.keyLast4 = last4;
        this.updatedAt = Instant.now();
    }

    void clearKey() {
        this.keySalt = null;
        this.encryptedKey = null;
        this.keyLast4 = null;
    }

    void recordTest(boolean ok, long latencyMs, String error) {
        this.lastTestAt = Instant.now();
        this.lastTestOk = ok;
        this.lastTestLatencyMs = latencyMs;
        this.lastTestError = error;
    }

    void setActive(boolean active) {
        this.active = active;
        this.updatedAt = Instant.now();
    }

    public UUID id() { return id; }
    public String scope() { return scope; }
    public UUID projectId() { return projectId; }
    public String provider() { return provider; }
    public String baseUrl() { return baseUrl; }
    public String model() { return model; }
    public boolean active() { return active; }
    byte[] keySalt() { return keySalt; }
    byte[] encryptedKey() { return encryptedKey; }
    public boolean hasKey() { return encryptedKey != null; }
    public String keyLast4() { return keyLast4; }
    public Instant lastTestAt() { return lastTestAt; }
    public Boolean lastTestOk() { return lastTestOk; }
    public Long lastTestLatencyMs() { return lastTestLatencyMs; }
    public String lastTestError() { return lastTestError; }
    public Instant updatedAt() { return updatedAt; }
}
