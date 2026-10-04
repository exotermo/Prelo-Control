package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
public class HermesOrchestrator {
 private final AgentExecutor executor;
 private final ContextResolver contextResolver;
 private final ContextSnapshotRepository contextSnapshots;
 private final AgentDefinitionRegistry agents;
 public HermesOrchestrator(AgentExecutor executor, ContextResolver contextResolver, ContextSnapshotRepository contextSnapshots, AgentDefinitionRegistry agents) {
  this.executor = executor; this.contextResolver = contextResolver; this.contextSnapshots = contextSnapshots; this.agents = agents;
 }
 // Resolves the curated AgentDefinition for the task's persisted agentId. The task's agentId was
 // already validated against the catalog at creation time, but this still throws
 // UnknownAgentException if it somehow no longer resolves (catalog changed, etc.) instead of
 // silently falling back to anything.
 public AgentDefinition resolveAgent(Task task) { return agents.findRequired(task.agentId()); }
 // Resolves and persists the snapshot on its own, separate from the LLM call, so a caller can
 // attach contextSnapshotId to the Execution before risking a failure-prone call to the Gateway.
 public ContextSnapshot prepareContext(Task task) {
  return contextSnapshots.save(contextResolver.resolve(task));
 }
 // Compiles the prompt from an already-persisted snapshot and calls the LLM. Never resolves or
 // persists context itself, and never picks the agent itself — both already happened.
 public LlmClient.ChatResult execute(Task task, AgentDefinition agent, ContextSnapshot snapshot, String requestId) {
  return executor.execute(task, agent, snapshot, requestId);
 }
}
