package br.com.exotermo.hermes.gateway.security;

import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties("gateway.jwt")
public record GatewayJwtProperties(String secret, String issuer, String audience) {
    @Override public String toString() {
        return "GatewayJwtProperties[secret=<redacted>, issuer=" + issuer + ", audience=" + audience + "]";
    }
}
