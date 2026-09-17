package br.com.exotermo.hermes.app.gateway;

import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties("hermes.gateway")
public record GatewayProperties(String baseUrl, String jwtSecret, String issuer, String audience) { }
