package br.com.exotermo.hermes.app.application;
import java.util.List;
public interface LlmClient { String chat(String model, List<Message> messages, String taskId, String agentId); record Message(String role, String content) { } }
