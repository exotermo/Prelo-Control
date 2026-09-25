package br.com.exotermo.hermes.gateway.security;

import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import br.com.exotermo.hermes.gateway.application.ChatOrchestrationService;
import br.com.exotermo.hermes.gateway.api.LlmController;
import br.com.exotermo.hermes.gateway.llm.UnsupportedModelException;
import com.nimbusds.jose.JWSAlgorithm;
import com.nimbusds.jose.JWSHeader;
import com.nimbusds.jose.crypto.MACSigner;
import com.nimbusds.jwt.JWTClaimsSet;
import com.nimbusds.jwt.SignedJWT;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Date;
import java.util.List;
import java.util.UUID;
import javax.crypto.spec.SecretKeySpec;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.context.TestPropertySource;
import org.springframework.test.web.servlet.MockMvc;

/**
 * Exercises the OAuth2/JWT boundary end to end through the real filter chain: no mocked
 * Authentication objects, actual signed tokens go through the actual JwtDecoder.
 */
@WebMvcTest(LlmController.class)
@Import(SecurityConfiguration.class)
@TestPropertySource(properties = {
    "gateway.jwt.secret=" + LlmControllerSecurityTest.SECRET,
    "gateway.jwt.issuer=" + LlmControllerSecurityTest.ISSUER,
    "gateway.jwt.audience=" + LlmControllerSecurityTest.AUDIENCE
})
class LlmControllerSecurityTest {

    static final String SECRET = "test-only-secret-that-is-at-least-32-bytes-long";
    static final String ISSUER = "hermes-test-issuer";
    static final String AUDIENCE = "llm-gateway-test-audience";
    private static final String OTHER_SECRET = "a-completely-different-test-secret-0000000000000";
    private static final String CHAT_REQUEST_BODY = """
        {"modelProfile":"mock-echo","messages":[{"role":"user","content":"hi"}]}
        """;

    @Autowired private MockMvc mockMvc;
    @MockBean private ChatOrchestrationService chatService;

    @Test void rejectsRequestsWithNoToken() throws Exception {
        mockMvc.perform(chatRequest())
            .andExpect(status().isUnauthorized());
    }

    @Test void acceptsAValidTokenCarryingTheRequiredScope() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isOk());
    }

    @Test void rejectsATokenMissingTheRequiredScope() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("some:other-scope"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isForbidden());
    }

    @Test void rejectsATokenWithNoScopeAtAll() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of(), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isForbidden());
    }

    @Test void rejectsAnExpiredToken() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().minusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isUnauthorized());
    }

    @Test void rejectsATokenSignedWithAnUnknownSecret() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, OTHER_SECRET)))
            .andExpect(status().isUnauthorized());
    }

    @Test void rejectsATokenWithTheWrongAudience() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, "someone-elses-audience", SECRET)))
            .andExpect(status().isUnauthorized());
    }

    @Test void rejectsATokenWithTheWrongIssuer() throws Exception {
        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), "someone-elses-issuer", AUDIENCE, SECRET)))
            .andExpect(status().isUnauthorized());
    }

    @Test void mapsAnUnsupportedModelToABadRequest() throws Exception {
        when(chatService.execute(org.mockito.ArgumentMatchers.any(), org.mockito.ArgumentMatchers.anyString(), org.mockito.ArgumentMatchers.anyString(), org.mockito.ArgumentMatchers.any())).thenThrow(new UnsupportedModelException("mock-echo"));

        mockMvc.perform(chatRequest().header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isBadRequest());
    }

    @Test void rejectsAnEmptyRequestId() throws Exception {
        mockMvc.perform(chatRequest().header("X-Request-Id", "").header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isBadRequest());
    }

    @Test void rejectsARequestIdLongerThanTheAuditColumn() throws Exception {
        mockMvc.perform(chatRequest().header("X-Request-Id", "a".repeat(101)).header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isBadRequest());
    }

    @Test void rejectsUnexpectedCharactersInRequestId() throws Exception {
        mockMvc.perform(chatRequest().header("X-Request-Id", "invalid value").header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isBadRequest());
    }

    @Test void rejectsAMessageThatExceedsTheContentSizeLimit() throws Exception {
        String oversizedContent = "x".repeat(32_001);
        String body = "{\"modelProfile\":\"mock-echo\",\"messages\":[{\"role\":\"user\",\"content\":\"" + oversizedContent + "\"}]}";

        mockMvc.perform(post("/api/v1/llm/chat").contentType(MediaType.APPLICATION_JSON).content(body)
                .header("Authorization", "Bearer " + token(List.of("llm:invoke"), Instant.now().plusSeconds(60), ISSUER, AUDIENCE, SECRET)))
            .andExpect(status().isBadRequest());
    }

    private static org.springframework.test.web.servlet.request.MockHttpServletRequestBuilder chatRequest() {
        return post("/api/v1/llm/chat").contentType(MediaType.APPLICATION_JSON).content(CHAT_REQUEST_BODY);
    }

    private static String token(List<String> scopes, Instant expiry, String issuer, String audience, String secret) throws Exception {
        JWTClaimsSet claims = new JWTClaimsSet.Builder().subject("test-client").issuer(issuer).audience(audience)
            .claim("client_id", "test-client").claim("scope", scopes)
            .issueTime(Date.from(Instant.now().minusSeconds(5))).expirationTime(Date.from(expiry))
            .jwtID(UUID.randomUUID().toString()).build();
        SignedJWT jwt = new SignedJWT(new JWSHeader(JWSAlgorithm.HS256), claims);
        jwt.sign(new MACSigner(new SecretKeySpec(secret.getBytes(StandardCharsets.UTF_8), "HmacSHA256")));
        return jwt.serialize();
    }
}
