package br.com.exotermo.hermes.app.infrastructure.gateway;
import br.com.exotermo.hermes.app.application.LlmClient; import br.com.exotermo.hermes.app.gateway.*; import java.util.*; import org.springframework.stereotype.Component;
@Component public class GatewayLlmClient implements LlmClient {
 private final LanguageModelGateway gateway;
 public GatewayLlmClient(LanguageModelGateway gateway){this.gateway=gateway;}
 // requestId comes from the caller — never generated here. The caller (ultimately
 // ExecuteTaskUseCase) is the one who needs it to correlate the Execution with the call it made.
 public ChatResult chat(String modelProfile, List<Message> messages, String requestId, String taskId, String agentId){
  GatewayContracts.ChatResponse response = gateway.chat(new GatewayContracts.ChatRequest(modelProfile, messages.stream().map(m->new GatewayContracts.Message(m.role(),m.content())).toList(), null, Map.of("taskId",taskId,"agentId",agentId)), requestId);
  return new ChatResult(response.content(), response.provider(), response.model(), response.requestId());
 }
}
