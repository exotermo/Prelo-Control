package br.com.exotermo.prelo.gateway.application;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

import br.com.exotermo.prelo.gateway.audit.AuditService;
import br.com.exotermo.prelo.gateway.llm.*;
import br.com.exotermo.prelo.gateway.provider.*;
import br.com.exotermo.prelo.gateway.routing.FallbackPlanResolver;
import br.com.exotermo.prelo.gateway.routing.GatewayRoutingProperties;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class ChatOrchestrationServiceTest {

    private static GatewayRoutingProperties routingWith(Map<String, GatewayRoutingProperties.ProfileConfig> profiles, Duration totalBudget) {
        return new GatewayRoutingProperties(profiles, totalBudget, true);
    }

    private static LLMRequest requestFor(String profile) {
        return new LLMRequest(profile, List.of(new LLMMessage("user", "hi")), null, null, null);
    }

    private static LLMResponse successResponse(String provider, String model) {
        return new LLMResponse(UUID.randomUUID(), provider, model, LLMResponse.KIND_FINAL, "ok", null, null, null, new LLMResponse.Usage(1, 1, 2), 0, null, List.of());
    }

    @Test void aTransientFailureOnThePrimaryCandidateFallsBackToTheSecondary() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        when(primary.supports("model-a")).thenReturn(true);
        when(primary.execute(eq("model-a"), any())).thenThrow(new ProviderUnavailableException("down"));

        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");
        when(secondary.supports("model-b")).thenReturn(true);
        when(secondary.execute(eq("model-b"), any())).thenReturn(successResponse("secondary", "model-b"));

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a", "secondary:model-b"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary, secondary), mock(AuditService.class));

        LLMResponse response = service.execute(requestFor("general-chat"), "req-1", "subject", "client");

        assertEquals("secondary", response.provider());
        assertEquals(2, response.attempts().size());
        assertEquals("success", response.attempts().get(1).outcome());
    }

    @Test void aRejectedRequestDoesNotFallBack() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        when(primary.supports("model-a")).thenReturn(true);
        when(primary.execute(eq("model-a"), any())).thenThrow(new ProviderRejectedRequestException("bad request"));
        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a", "secondary:model-b"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary, secondary), mock(AuditService.class));

        FallbackExhaustedException exception = assertThrows(FallbackExhaustedException.class, () -> service.execute(requestFor("general-chat"), "req-1", "s", "c"));
        assertEquals(1, exception.attempts().size());
        verify(secondary, never()).execute(anyString(), any());
    }

    @Test void anAuthenticationFailureDoesNotFallBack() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        when(primary.supports("model-a")).thenReturn(true);
        when(primary.execute(eq("model-a"), any())).thenThrow(new ProviderAuthenticationException("bad creds"));
        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a", "secondary:model-b"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary, secondary), mock(AuditService.class));

        assertThrows(FallbackExhaustedException.class, () -> service.execute(requestFor("general-chat"), "req-1", "s", "c"));
        verify(secondary, never()).execute(anyString(), any());
    }

    @Test void whenAllCandidatesFailATypedFallbackExhaustedExceptionIsThrown() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        when(primary.supports("model-a")).thenReturn(true);
        when(primary.execute(eq("model-a"), any())).thenThrow(new ProviderTimeoutException("slow"));
        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");
        when(secondary.supports("model-b")).thenReturn(true);
        when(secondary.execute(eq("model-b"), any())).thenThrow(new ProviderUnavailableException("down"));

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a", "secondary:model-b"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary, secondary), mock(AuditService.class));

        FallbackExhaustedException exception = assertThrows(FallbackExhaustedException.class, () -> service.execute(requestFor("general-chat"), "req-1", "s", "c"));
        assertEquals(2, exception.attempts().size());
        assertInstanceOf(ProviderUnavailableException.class, exception.lastFailure());
    }

    @Test void aDevProfileCanUseMockAsItsOnlyExplicitlyConfiguredCandidate() {
        LLMProvider mockProvider = mock(LLMProvider.class);
        when(mockProvider.id()).thenReturn("mock");
        when(mockProvider.supports("mock-echo")).thenReturn(true);
        when(mockProvider.execute(eq("mock-echo"), any())).thenReturn(successResponse("mock", "mock-echo"));

        var routing = routingWith(Map.of("dev-chat", new GatewayRoutingProperties.ProfileConfig(List.of("mock:mock-echo"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(mockProvider), mock(AuditService.class));

        LLMResponse response = service.execute(requestFor("dev-chat"), "req-1", "s", "c");
        assertEquals("mock", response.provider());
    }

    @Test void bothThePrimaryAttemptAndTheFallbackAreAudited() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        when(primary.supports("model-a")).thenReturn(true);
        when(primary.execute(eq("model-a"), any())).thenThrow(new ProviderTimeoutException("slow"));
        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");
        when(secondary.supports("model-b")).thenReturn(true);
        when(secondary.execute(eq("model-b"), any())).thenReturn(successResponse("secondary", "model-b"));

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a", "secondary:model-b"))), Duration.ofSeconds(20));
        AuditService audit = mock(AuditService.class);
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary, secondary), audit);

        service.execute(requestFor("general-chat"), "req-1", "s", "c");

        verify(audit).record(eq("PROVIDER_ATTEMPT"), eq("req-1"), anyString(), anyString(), eq("general-chat"), eq("primary"), eq("model-a"), eq(1), anyLong(), anyString());
        verify(audit).record(eq("PROVIDER_ATTEMPT"), eq("req-1"), anyString(), anyString(), eq("general-chat"), eq("secondary"), eq("model-b"), eq(2), anyLong(), anyString());
    }

    @Test void theTotalBudgetStopsFurtherAttemptsOnceExhausted() {
        LLMProvider slow = mock(LLMProvider.class);
        when(slow.id()).thenReturn("slow");
        when(slow.supports("model-a")).thenReturn(true);
        when(slow.execute(eq("model-a"), any())).thenAnswer(invocation -> {
            Thread.sleep(150);
            throw new ProviderTimeoutException("slow");
        });
        LLMProvider secondary = mock(LLMProvider.class);
        when(secondary.id()).thenReturn("secondary");
        when(secondary.supports("model-b")).thenReturn(true);

        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("slow:model-a", "secondary:model-b"))), Duration.ofMillis(100));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(slow, secondary), mock(AuditService.class));

        assertThrows(FallbackExhaustedException.class, () -> service.execute(requestFor("general-chat"), "req-1", "s", "c"));
        verify(secondary, never()).execute(anyString(), any());
    }

    @Test void anUnknownProfileFailsBeforeAnyProviderIsTried() {
        LLMProvider primary = mock(LLMProvider.class);
        when(primary.id()).thenReturn("primary");
        var routing = routingWith(Map.of("general-chat", new GatewayRoutingProperties.ProfileConfig(List.of("primary:model-a"))), Duration.ofSeconds(20));
        var service = new ChatOrchestrationService(new FallbackPlanResolver(routing), List.of(primary), mock(AuditService.class));

        assertThrows(UnsupportedModelException.class, () -> service.execute(requestFor("no-such-profile"), "req-1", "s", "c"));
        verify(primary, never()).execute(anyString(), any());
    }
}
