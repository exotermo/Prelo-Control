package br.com.exotermo.hermes.app.application;
import br.com.exotermo.hermes.app.domain.*;
import java.util.ArrayList;
import java.util.List;

// Builds a ContextSnapshot from manual sources only (no automatic memory, no RAG, no embeddings
// in this iteration). Pure function: it never persists anything itself — the caller decides when
// to save the resulting snapshot via ContextSnapshotRepository.
public class ContextResolver {
 private final ManualContextRepository manualContext;
 private final ContextSnapshotRepository snapshots;
 public ContextResolver(ManualContextRepository manualContext, ContextSnapshotRepository snapshots) { this.manualContext = manualContext; this.snapshots = snapshots; }

 public ContextSnapshot resolve(Task task) {
  List<ContextSnapshotItem> items = new ArrayList<>();
  items.add(new ContextSnapshotItem("description", task.description(), ContextSourceType.MANUAL, "task-description", 0));
  int order = 1;
  for (ManualContextItem item : manualContext.findByTaskId(task.id())) {
   items.add(new ContextSnapshotItem(item.name(), item.content(), ContextSourceType.MANUAL, "manual-input", order++));
  }
  int nextVersion = snapshots.findLatestByTaskId(task.id()).map(s -> s.version() + 1).orElse(1);
  return ContextSnapshot.resolve(task.id(), nextVersion, items);
 }
}
