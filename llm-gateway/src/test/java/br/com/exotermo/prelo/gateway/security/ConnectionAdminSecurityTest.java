package br.com.exotermo.prelo.gateway.security;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.content;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import br.com.exotermo.prelo.gateway.api.ConnectionAdminController;
import br.com.exotermo.prelo.gateway.connection.ConnectionService;
import br.com.exotermo.prelo.gateway.connection.ConnectionValidationException;
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
import org.hamcrest.Matchers;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.context.TestPropertySource;
import org.springframework.test.web.servlet.MockMvc;

/** The admin surface must refuse the agents' llm:invoke token and never echo a submitted key. */
@WebMvcTest(ConnectionAdminController.class)
@Import(SecurityConfiguration.class)
@TestPropertySource(properties = {
    "gateway.jwt.secret=" + ConnectionAdminSecurityTest.SECRET,
    "gateway.jwt.issuer=iss",
    "gateway.jwt.audience=aud"
})
class ConnectionAdminSecurityTest {
    static final String SECRET = "test-only-secret-that-is-at-least-32-bytes-long";

    @Autowired private MockMvc mockMvc;
    @MockBean private ConnectionService connections;

    private static String token(List<String> scopes) throws Exception {
        JWTClaimsSet claims = new JWTClaimsSet.Builder().subject("prelo-core").issuer("iss").audience("aud")
            .claim("client_id", "prelo-core").claim("scope", scopes)
            .issueTime(Date.from(Instant.now().minusSeconds(5))).expirationTime(Date.from(Instant.now().plusSeconds(60)))
            .jwtID(UUID.randomUUID().toString()).build();
        SignedJWT jwt = new SignedJWT(new JWSHeader(JWSAlgorithm.HS256), claims);
        jwt.sign(new MACSigner(new SecretKeySpec(SECRET.getBytes(StandardCharsets.UTF_8), "HmacSHA256")));
        return jwt.serialize();
    }

    @Test void theAgentInvokeScopeCannotReachTheAdminSurface() throws Exception {
        mockMvc.perform(get("/api/v1/admin/connections/instance").header("Authorization", "Bearer " + token(List.of("llm:invoke"))))
            .andExpect(status().isForbidden());
    }

    @Test void theAdminScopeCanReadAndNoTokenIsRejected() throws Exception {
        when(connections.summary(null)).thenReturn(new ConnectionService.ConnectionSummary(false, "INSTANCE", null, null, null, null,
            false, false, null, null, null, null, null, null));
        mockMvc.perform(get("/api/v1/admin/connections/instance").header("Authorization", "Bearer " + token(List.of("llm:admin"))))
            .andExpect(status().isOk());
        mockMvc.perform(get("/api/v1/admin/connections/instance")).andExpect(status().isUnauthorized());
    }

    @Test void validationErrorsAreReadableAndNeverEchoTheKey() throws Exception {
        when(connections.save(any(), any())).thenThrow(new ConnectionValidationException("o provedor recusou a chave"));
        mockMvc.perform(put("/api/v1/admin/connections/instance").header("Authorization", "Bearer " + token(List.of("llm:admin")))
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"provider\":\"anthropic\",\"model\":\"m\",\"apiKey\":\"sk-ant-super-secret-value\"}"))
            .andExpect(status().isBadRequest())
            .andExpect(jsonPath("$.message").value("o provedor recusou a chave"))
            .andExpect(content().string(Matchers.not(Matchers.containsString("sk-ant-super-secret-value"))));
    }
}
