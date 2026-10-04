package br.com.exotermo.prelo.gateway.connection;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

import br.com.exotermo.prelo.gateway.provider.ProviderAuthenticationException;
import java.time.Duration;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;

class ConnectionServiceTest {
    private ProviderConnectionRepository repository;
    private ProviderWireClient wire;
    private ConnectionKeyCipher cipher;
    private ConnectionService service;
    private final ConnectionProperties properties = new ConnectionProperties("/nonexistent", false, Duration.ofSeconds(30), Duration.ofSeconds(5), 512);

    @BeforeEach void setUp() {
        repository = mock(ProviderConnectionRepository.class);
        wire = mock(ProviderWireClient.class);
        cipher = new ConnectionKeyCipher(new byte[32]);
        service = new ConnectionService(repository, properties, wire, null, cipher);
        when(repository.save(any())).thenAnswer(inv -> inv.getArgument(0));
        when(repository.findFirstByScope(any())).thenReturn(Optional.empty());
        when(repository.findByScopeAndProjectId(any(), any())).thenReturn(Optional.empty());
    }

    @Test void savesOnlyAfterAPassingTestAndStoresTheKeySealed() {
        when(wire.listModels(eq("anthropic"), anyString(), eq("sk-ant-0123456789abcdef"), any())).thenReturn(List.of("claude-x"));
        var summary = service.save(null, new ConnectionService.SaveCommand("anthropic", null, "claude-x", "sk-ant-0123456789abcdef"));

        var captor = ArgumentCaptor.forClass(ProviderConnection.class);
        verify(repository).save(captor.capture());
        ProviderConnection saved = captor.getValue();
        assertTrue(saved.hasKey());
        assertNotEquals("sk-ant-0123456789abcdef", new String(saved.encryptedKey()));
        assertEquals("sk-ant-0123456789abcdef", cipher.open(saved.keySalt(), saved.encryptedKey(), saved.associatedData()));
        assertEquals("cdef", summary.keyLast4());
        assertEquals("https://api.anthropic.com", summary.baseUrl());
    }

    @Test void aRejectedKeyIsNeverPersisted() {
        when(wire.listModels(any(), any(), any(), any())).thenThrow(new ProviderAuthenticationException("no"));
        var error = assertThrows(ConnectionValidationException.class,
            () -> service.save(null, new ConnectionService.SaveCommand("openai", null, "gpt-x", "sk-bad-0123456789")));
        assertEquals("o provedor recusou a chave", error.getMessage());
        verify(repository, never()).save(any());
    }

    @Test void aModelTheKeyCannotUseIsRefused() {
        when(wire.listModels(any(), any(), any(), any())).thenReturn(List.of("gpt-a"));
        assertThrows(ConnectionValidationException.class,
            () -> service.save(null, new ConnectionService.SaveCommand("openai", null, "gpt-z", "sk-0123456789abcdef")));
    }

    @Test void switchingProviderRequiresANewKey() {
        when(wire.listModels(any(), any(), any(), any())).thenReturn(List.of("claude-x", "gpt-x"));
        service.save(null, new ConnectionService.SaveCommand("anthropic", null, "claude-x", "sk-ant-0123456789abcdef"));
        var captor = ArgumentCaptor.forClass(ProviderConnection.class);
        verify(repository).save(captor.capture());
        when(repository.findFirstByScope("INSTANCE")).thenReturn(Optional.of(captor.getValue()));

        assertThrows(ConnectionValidationException.class,
            () -> service.save(null, new ConnectionService.SaveCommand("openai", null, "gpt-x", null)));
    }

    @Test void routesToTheActiveProjectConnectionElseTheInstance() {
        UUID project = UUID.randomUUID();
        var instance = new ProviderConnection("INSTANCE", null);
        instance.configure("anthropic", "https://api.anthropic.com", "claude-instance");
        var own = new ProviderConnection("PROJECT", project);
        own.configure("openai", "https://api.openai.com/v1", "gpt-own");
        when(repository.findFirstByScope("INSTANCE")).thenReturn(Optional.of(instance));
        when(repository.findByScopeAndProjectId("PROJECT", project)).thenReturn(Optional.of(own));

        assertEquals("gpt-own", service.route(project).orElseThrow().model());
        own.setActive(false);
        assertEquals("claude-instance", service.route(project).orElseThrow().model());
        assertEquals("claude-instance", service.route(null).orElseThrow().model());
    }

    @Test void withoutAMasterKeyRoutingFallsThroughAndAdminCallsAreRefused() {
        var disabled = new ConnectionService(repository, properties, wire, null, null);
        assertTrue(disabled.route(UUID.randomUUID()).isEmpty());
        assertThrows(ConnectionService.ConnectionsDisabledException.class, () -> disabled.summary(null));
    }

    // --- Fase X: subscription CLIs (hybrid use) ---

    private ConnectionService withCli(CliRunnerClient cli) {
        var props = new ConnectionProperties("/nonexistent", false, Duration.ofSeconds(30), Duration.ofSeconds(5), 512,
            "http://cli-runner:8099", "t".repeat(40), Duration.ofSeconds(60));
        return new ConnectionService(repository, props, wire, null, cipher, cli);
    }

    @Test void aCliConnectionOnlyServesTheOwnersOwnWork() {
        UUID project = UUID.randomUUID();
        var instance = new ProviderConnection("INSTANCE", null);
        instance.configure("openai", "https://api.openai.com/v1", "gpt-mini");
        var own = new ProviderConnection("PROJECT", project);
        own.configure("claude_cli", "cli-runner", "sonnet");
        when(repository.findFirstByScope("INSTANCE")).thenReturn(Optional.of(instance));
        when(repository.findByScopeAndProjectId("PROJECT", project)).thenReturn(Optional.of(own));

        assertEquals("sonnet", service.route(project, "MANUAL").orElseThrow().model());
        assertEquals("gpt-mini", service.route(project, "MESSAGING").orElseThrow().model(), "WhatsApp must use the API connection");
        assertEquals("gpt-mini", service.route(project, null).orElseThrow().model(), "unknown origin is never treated as the owner");
    }

    @Test void withOnlyACliInstanceConnectionWhatsAppFallsBackToTheStaticProfiles() {
        var instance = new ProviderConnection("INSTANCE", null);
        instance.configure("codex_cli", "cli-runner", "default");
        when(repository.findFirstByScope("INSTANCE")).thenReturn(Optional.of(instance));
        assertTrue(service.route(null, "MESSAGING").isEmpty());
        assertTrue(service.route(null, "MANUAL").isPresent());
    }

    @Test void theInstanceConnectionCanBeSwitchedOff() {
        var instance = new ProviderConnection("INSTANCE", null);
        instance.configure("openai", "https://api.openai.com/v1", "gpt-mini");
        when(repository.findFirstByScope("INSTANCE")).thenReturn(Optional.of(instance));
        service.setActive(null, false);
        assertTrue(service.route(null, "MESSAGING").isEmpty(), "an inactive instance connection must not answer");
    }

    @Test void aCliConnectionIsSavedWithoutAKeyAfterARealAnswer() {
        CliRunnerClient cli = mock(CliRunnerClient.class);
        when(cli.configured()).thenReturn(true);
        when(cli.status("claude_cli")).thenReturn(new CliRunnerClient.EngineStatus(true, "login"));
        var svc = withCli(cli);
        var summary = svc.save(UUID.randomUUID(), new ConnectionService.SaveCommand("claude_cli", "https://evil.example", "sonnet", "sk-ignored"));
        assertFalse(summary.hasKey(), "a CLI connection never stores a key");
        assertEquals("cli-runner", summary.baseUrl(), "the user cannot point a CLI connection anywhere");
        verify(cli).chat(eq("claude_cli"), eq("sonnet"), any(), any());
    }

    @Test void aLoggedOutCliIsRefusedWithTheLoginCommand() {
        CliRunnerClient cli = mock(CliRunnerClient.class);
        when(cli.configured()).thenReturn(true);
        when(cli.status("codex_cli")).thenReturn(new CliRunnerClient.EngineStatus(false, "docker compose exec -it cli-runner codex login --device-auth"));
        var error = assertThrows(ConnectionValidationException.class,
            () -> withCli(cli).save(null, new ConnectionService.SaveCommand("codex_cli", null, "default", null)));
        assertTrue(error.getMessage().contains("codex login --device-auth"), error.getMessage());
        verify(repository, never()).save(any());
    }

    @Test void noCreditIsToldApartFromRateLimiting() {
        when(wire.listModels(any(), any(), any(), any())).thenThrow(new br.com.exotermo.prelo.gateway.provider.ProviderQuotaExceededException("q"));
        var result = service.test("openai", null, "sk-0123456789abcdef");
        assertEquals("a conta do provedor está sem crédito — adicione saldo no painel de cobrança dele", result.error());
    }
}
