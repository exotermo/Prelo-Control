package br.com.exotermo.hermes.app.application;
import java.util.List;
public interface LlmClient {
 ChatResult chat(String modelProfile, List<Message> messages, String requestId, String taskId, String agentId);
 record Message(String role, String content) { }
 // provider/model here are what was EFFECTIVELY used (from the Gateway's response), not the
 // requested modelProfile — the two can differ once fallback picks a secondary candidate.
 record ChatResult(String content, String provider, String model, String requestId) { }
}
