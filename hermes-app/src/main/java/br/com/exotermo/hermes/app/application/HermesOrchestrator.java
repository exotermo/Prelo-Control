package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
public class HermesOrchestrator {
 private final AgentExecutor executor;
 public HermesOrchestrator(AgentExecutor executor) { this.executor = executor; }
 public String execute(Task task) { return executor.execute(task, Agent.general(), new Context(java.util.List.of(new ContextItem("task", task.description()))), new Directive("You are Hermes GeneralAgent. Respond concisely and helpfully.")); }
}
