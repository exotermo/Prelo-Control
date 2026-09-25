package br.com.exotermo.hermes.app.api;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoInteractions;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import br.com.exotermo.hermes.app.application.*;
import br.com.exotermo.hermes.app.domain.*;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.http.MediaType;
import org.springframework.orm.ObjectOptimisticLockingFailureException;
import org.springframework.test.web.servlet.MockMvc;

@WebMvcTest(TaskController.class)
class TaskControllerTest {

    @Autowired private MockMvc mockMvc;
    @MockBean private CreateTaskUseCase createTaskUseCase;
    @MockBean private ExecuteTaskUseCase executeTaskUseCase;
    @MockBean private TaskRepository tasks;

    @Test void rejectsExcessiveAggregateContextBudgetBeforePersistingAnything() throws Exception {
        StringBuilder body = new StringBuilder("{\"description\":\"summarize this\",\"context\":[");
        for (int i = 0; i < 4; i++) {
            if (i > 0) body.append(",");
            body.append("{\"name\":\"item").append(i).append("\",\"content\":\"").append("x".repeat(7999)).append("\"}");
        }
        body.append("]}");

        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content(body.toString()))
            .andExpect(status().isBadRequest());

        verifyNoInteractions(createTaskUseCase);
    }

    @Test void rejectsMoreThanTheAllowedNumberOfManualContextItems() throws Exception {
        StringBuilder body = new StringBuilder("{\"description\":\"summarize this\",\"context\":[");
        for (int i = 0; i < ContextSnapshot.MAX_ITEMS + 1; i++) {
            if (i > 0) body.append(",");
            body.append("{\"name\":\"item").append(i).append("\",\"content\":\"x\"}");
        }
        body.append("]}");

        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content(body.toString()))
            .andExpect(status().isBadRequest());

        verifyNoInteractions(createTaskUseCase);
    }

    @Test void mapsInvalidTaskTransitionTo409() throws Exception {
        UUID taskId = UUID.randomUUID();
        when(executeTaskUseCase.execute(any())).thenThrow(new InvalidTaskTransitionException(TaskStatus.COMPLETED, TaskStatus.RUNNING));

        mockMvc.perform(post("/api/v1/tasks/" + taskId + "/execute"))
            .andExpect(status().isConflict());
    }

    @Test void mapsOptimisticLockConflictTo409() throws Exception {
        UUID taskId = UUID.randomUUID();
        when(executeTaskUseCase.execute(any())).thenThrow(new ObjectOptimisticLockingFailureException(Execution.class, taskId));

        mockMvc.perform(post("/api/v1/tasks/" + taskId + "/execute"))
            .andExpect(status().isConflict());
    }

    @Test void mapsTaskNotFoundTo404OnExecute() throws Exception {
        UUID taskId = UUID.randomUUID();
        when(executeTaskUseCase.execute(any())).thenThrow(new ExecuteTaskUseCase.TaskNotFoundException(new TaskId(taskId)));

        mockMvc.perform(post("/api/v1/tasks/" + taskId + "/execute"))
            .andExpect(status().isNotFound());
    }

    @Test void rejectsABlankDescriptionWith400() throws Exception {
        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content("{\"description\":\"\"}"))
            .andExpect(status().isBadRequest());

        verifyNoInteractions(createTaskUseCase);
    }

    @Test void omittingAgentIdLetsTheUseCaseDefaultToGeneral() throws Exception {
        when(createTaskUseCase.create(eq("summarize this"), any(), isNull()))
            .thenReturn(new Task(TaskId.newId(), "summarize this", TaskStatus.CREATED, java.time.Instant.now(), new AgentId("general"), 0L));

        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content("{\"description\":\"summarize this\"}"))
            .andExpect(status().isCreated());

        verify(createTaskUseCase).create(eq("summarize this"), any(), isNull());
    }

    @Test void anExplicitAgentIdIsPassedThroughToTheUseCase() throws Exception {
        when(createTaskUseCase.create(eq("summarize this"), any(), eq(new AgentId("concise"))))
            .thenReturn(new Task(TaskId.newId(), "summarize this", TaskStatus.CREATED, java.time.Instant.now(), new AgentId("concise"), 0L));

        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content("{\"description\":\"summarize this\",\"agentId\":\"concise\"}"))
            .andExpect(status().isCreated());

        verify(createTaskUseCase).create(eq("summarize this"), any(), eq(new AgentId("concise")));
    }

    @Test void mapsAnUnknownAgentTo400() throws Exception {
        when(createTaskUseCase.create(eq("summarize this"), any(), eq(new AgentId("ghost"))))
            .thenThrow(new UnknownAgentException(new AgentId("ghost")));

        mockMvc.perform(post("/api/v1/tasks").contentType(MediaType.APPLICATION_JSON).content("{\"description\":\"summarize this\",\"agentId\":\"ghost\"}"))
            .andExpect(status().isBadRequest());
    }
}
