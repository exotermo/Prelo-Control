package br.com.exotermo.hermes.app;
import br.com.exotermo.hermes.app.application.*; import org.springframework.context.annotation.*;
@Configuration public class CoreConfiguration { @Bean AgentExecutor agentExecutor(LlmClient c){return new AgentExecutor(c);}@Bean HermesOrchestrator hermesOrchestrator(AgentExecutor e){return new HermesOrchestrator(e);}@Bean CreateTaskUseCase createTaskUseCase(TaskRepository r){return new CreateTaskUseCase(r);}@Bean ExecuteTaskUseCase executeTaskUseCase(TaskRepository t,ExecutionRepository e,HermesOrchestrator o){return new ExecuteTaskUseCase(t,e,o);} }
