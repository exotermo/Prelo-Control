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
}
