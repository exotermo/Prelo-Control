package br.com.exotermo.hermes.app.domain;
import static org.junit.jupiter.api.Assertions.*;
import org.junit.jupiter.api.Test;

class TaskTest {
 @Test void movesFromCreatedToRunningToCompleted() {
  Task task = Task.create("do something");
  Task running = task.running();
  Task completed = running.completed();
  assertEquals(TaskStatus.RUNNING, running.status());
  assertEquals(TaskStatus.COMPLETED, completed.status());
 }

 @Test void refusesToStartATaskThatIsAlreadyRunning() {
  Task running = Task.create("do something").running();
  assertThrows(InvalidTaskTransitionException.class, running::running);
 }

 @Test void refusesToStartATaskThatIsAlreadyCompleted() {
  Task completed = Task.create("do something").running().completed();
  assertThrows(InvalidTaskTransitionException.class, completed::running);
 }

 @Test void refusesToStartATaskThatHasAlreadyFailed() {
  Task failed = Task.create("do something").running().failed();
  assertThrows(InvalidTaskTransitionException.class, failed::running);
 }

 @Test void refusesToCompleteATaskThatNeverStarted() {
  Task created = Task.create("do something");
  assertThrows(InvalidTaskTransitionException.class, created::completed);
 }

 @Test void refusesToFailATaskThatNeverStarted() {
  Task created = Task.create("do something");
  assertThrows(InvalidTaskTransitionException.class, created::failed);
 }

 @Test void allowsStartingAQueuedTask() {
  Task queued = new Task(TaskId.newId(), "do something", TaskStatus.QUEUED, java.time.Instant.now(), new AgentId("general"), 0L);
  assertEquals(TaskStatus.RUNNING, queued.running().status());
 }
}
