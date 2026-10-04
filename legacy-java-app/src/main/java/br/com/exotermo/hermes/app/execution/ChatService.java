package br.com.exotermo.hermes.app.execution;

import br.com.exotermo.hermes.app.gateway.*;
import br.com.exotermo.hermes.app.gateway.GatewayContracts.*;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.springframework.stereotype.Service;

@Service
public class ChatService {
    private final LanguageModelGateway gateway;
    private final HermesExecutionRepository repository;
    ChatService(LanguageModelGateway gateway, HermesExecutionRepository repository) { this.gateway = gateway; this.repository = repository; }
    public ChatResponse chat(ChatCommand command, String requestId) {
        try {
            ChatResponse response = gateway.chat(new ChatRequest(command.model(), command.messages(), command.parameters(), Map.of("taskId", safe(command.taskId()), "agentId", safe(command.agentId()))), requestId);
            repository.save(new HermesExecution(requestId, command.taskId(), command.agentId(), command.model(), response.provider(), response.durationMs(), "COMPLETED"));
            return response;
        } catch (RuntimeException exception) {
            repository.save(new HermesExecution(requestId, command.taskId(), command.agentId(), command.model(), null, null, "FAILED"));
            throw exception;
        }
    }
    private String safe(String value) { return value == null ? "" : value; }
    public record ChatCommand(String model, List<Message> messages, Parameters parameters, String taskId, String agentId) { }
}
