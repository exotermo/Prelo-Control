package br.com.exotermo.prelo.gateway.connection;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.bind.ConstructorBinding;
import org.springframework.boot.context.properties.bind.DefaultValue;

/**
 * masterKeyFile: path to the Docker secret holding the 32-byte (Base64) master key. When the file
 * is absent, connections are simply disabled (admin endpoints answer 503) and routing keeps using
 * the static profiles — the gateway still boots.
 * allowPrivateUrls: lets an OpenAI-compatible base URL point at private/loopback addresses (e.g. a
 * local Ollama) and use plain http. Off by default: the gateway sits on the internal network, so a
 * user-supplied URL must not be able to reach Postgres or other internal services.
 */
@ConfigurationProperties(prefix = "gateway.connections")
public record ConnectionProperties(
        @DefaultValue("/run/secrets/gateway_connections_key") String masterKeyFile,
        @DefaultValue("false") boolean allowPrivateUrls,
        @DefaultValue("60s") Duration chatTimeout,
        @DefaultValue("10s") Duration testTimeout,
        @DefaultValue("1024") int maxTokens,
        // Fase X: cli-runner (subscription CLIs). Empty URL/token = CLI connections unavailable.
        @DefaultValue("") String cliRunnerUrl,
        @DefaultValue("") String cliRunnerToken,
        @DefaultValue("180s") Duration cliTimeout) {

    // Two constructors: tell Spring which one binds the properties (the canonical one).
    @ConstructorBinding
    public ConnectionProperties { }

    public ConnectionProperties(String masterKeyFile, boolean allowPrivateUrls, Duration chatTimeout, Duration testTimeout, int maxTokens) {
        this(masterKeyFile, allowPrivateUrls, chatTimeout, testTimeout, maxTokens, "", "", Duration.ofSeconds(180));
    }

    @Override
    public String toString() {
        return "ConnectionProperties{masterKeyFile=[redacted], allowPrivateUrls=" + allowPrivateUrls + ", chatTimeout=" + chatTimeout
            + ", testTimeout=" + testTimeout + ", maxTokens=" + maxTokens + ", cliRunnerUrl=" + cliRunnerUrl
            + ", cliRunnerToken=[redacted], cliTimeout=" + cliTimeout + "}";
    }
}
