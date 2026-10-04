package br.com.exotermo.prelo.gateway.connection;

import static org.junit.jupiter.api.Assertions.*;

import java.time.Duration;
import org.junit.jupiter.api.Test;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;
import org.springframework.context.annotation.Configuration;

/**
 * Binds ConnectionProperties the way the running gateway does. Unit tests build the record by
 * hand, so a second constructor once broke the real boot without any test noticing (Fase X).
 */
class ConnectionPropertiesBindingTest {
    @Configuration
    @EnableConfigurationProperties(ConnectionProperties.class)
    static class Config { }

    @Test void bindsFromEnvironmentLikeTheRunningGateway() {
        new ApplicationContextRunner().withUserConfiguration(Config.class)
            .withPropertyValues("gateway.connections.cli-runner-url=http://cli-runner:8099",
                "gateway.connections.cli-runner-token=" + "t".repeat(40), "gateway.connections.cli-timeout=90s")
            .run(context -> {
                assertNull(context.getStartupFailure());
                ConnectionProperties properties = context.getBean(ConnectionProperties.class);
                assertEquals("http://cli-runner:8099", properties.cliRunnerUrl());
                assertEquals(Duration.ofSeconds(90), properties.cliTimeout());
                assertEquals(1024, properties.maxTokens());
            });
    }
}
