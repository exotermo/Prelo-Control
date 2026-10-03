package dev.hermes.bridge;

import dev.hermes.bridge.config.BridgeProperties;
import dev.hermes.bridge.persistence.OwnerContactStore;
import org.springframework.boot.ApplicationArguments;
import org.springframework.boot.ApplicationRunner;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.scheduling.annotation.EnableAsync;
import org.springframework.scheduling.annotation.EnableScheduling;

@SpringBootApplication
@EnableAsync
@EnableScheduling
@EnableConfigurationProperties(BridgeProperties.class)
public class BridgeApplication {
    public static void main(String[] args) {
        SpringApplication.run(BridgeApplication.class, args);
    }

    // Fase G2: BRIDGE_OWNER_CONTACTS only ever seeds an empty owner_contacts table, once, on
    // boot — after that the table (managed via the admin API / hermes-dashboard) is the only
    // source of truth. See OwnerContactStore.seedIfEmpty.
    @Bean
    ApplicationRunner seedOwnerContacts(OwnerContactStore store, BridgeProperties properties) {
        return (ApplicationArguments args) -> store.seedIfEmpty(properties.ownerContacts());
    }
}
