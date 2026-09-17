package br.com.exotermo.hermes.app.gateway;

import br.com.exotermo.hermes.app.gateway.GatewayContracts.ChatRequest;
import br.com.exotermo.hermes.app.gateway.GatewayContracts.ChatResponse;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.nimbusds.jose.*;
import com.nimbusds.jose.crypto.MACSigner;
import com.nimbusds.jwt.JWTClaimsSet;
import com.nimbusds.jwt.SignedJWT;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Date;
import java.util.List;
import java.util.UUID;
import javax.crypto.spec.SecretKeySpec;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.stereotype.Component;

@Component
@EnableConfigurationProperties(GatewayProperties.class)
public class GatewayClient implements LanguageModelGateway {
    private final GatewayProperties properties;
    private final ObjectMapper objectMapper;
    private final HttpClient httpClient = HttpClient.newHttpClient();
    GatewayClient(GatewayProperties properties, ObjectMapper objectMapper) { this.properties = properties; this.objectMapper = objectMapper; }

    @Override public ChatResponse chat(ChatRequest request, String requestId) {
        try {
            String body = objectMapper.writeValueAsString(request);
            HttpRequest httpRequest = HttpRequest.newBuilder(URI.create(properties.baseUrl() + "/api/v1/llm/chat"))
                .header("Authorization", "Bearer " + serviceToken()).header("Content-Type", "application/json").header("X-Request-Id", requestId)
                .POST(HttpRequest.BodyPublishers.ofString(body)).build();
            HttpResponse<String> response = httpClient.send(httpRequest, HttpResponse.BodyHandlers.ofString());
            if (response.statusCode() != 200) throw new GatewayCallException(response.statusCode(), "Gateway rejected LLM request");
            return objectMapper.readValue(response.body(), ChatResponse.class);
        } catch (GatewayCallException exception) { throw exception; }
        catch (Exception exception) { throw new GatewayCallException(502, "Gateway communication failed", exception); }
    }

    private String serviceToken() throws JOSEException {
        Instant now = Instant.now();
        JWTClaimsSet claims = new JWTClaimsSet.Builder().subject("hermes-app").issuer(properties.issuer()).audience(properties.audience())
            .claim("client_id", "hermes-app").claim("scope", List.of("llm:invoke")).issueTime(Date.from(now)).expirationTime(Date.from(now.plusSeconds(60))).jwtID(UUID.randomUUID().toString()).build();
        SignedJWT jwt = new SignedJWT(new JWSHeader(JWSAlgorithm.HS256), claims);
        jwt.sign(new MACSigner(new SecretKeySpec(properties.jwtSecret().getBytes(StandardCharsets.UTF_8), "HmacSHA256")));
        return jwt.serialize();
    }
}
