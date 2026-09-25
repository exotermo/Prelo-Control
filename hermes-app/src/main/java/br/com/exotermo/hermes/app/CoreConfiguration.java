package br.com.exotermo.hermes.app;
import br.com.exotermo.hermes.app.application.*; import org.springframework.context.annotation.*;
@Configuration public class CoreConfiguration {
 @Bean AgentExecutor agentExecutor(LlmClient c){return new AgentExecutor(c);}
 @Bean ContextResolver contextResolver(ManualContextRepository m, ContextSnapshotRepository s){return new ContextResolver(m, s);}
 @Bean HermesOrchestrator hermesOrchestrator(AgentExecutor e, ContextResolver r, ContextSnapshotRepository s, AgentDefinitionRegistry a){return new HermesOrchestrator(e, r, s, a);}
 @Bean CreateTaskUseCase createTaskUseCase(TaskRepository t, ManualContextRepository m, AgentDefinitionRegistry a){return new CreateTaskUseCase(t, m, a);}
 @Bean ExecuteTaskUseCase executeTaskUseCase(TaskRepository t,ExecutionRepository e,HermesOrchestrator o){return new ExecuteTaskUseCase(t,e,o);}
}
