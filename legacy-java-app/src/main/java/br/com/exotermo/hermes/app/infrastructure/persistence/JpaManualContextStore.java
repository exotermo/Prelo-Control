package br.com.exotermo.hermes.app.infrastructure.persistence;
import br.com.exotermo.hermes.app.application.ManualContextRepository;
import br.com.exotermo.hermes.app.domain.*;
import java.util.*;
import org.springframework.stereotype.Repository;
@Repository public class JpaManualContextStore implements ManualContextRepository {
 private final SpringManualContextItemJpaRepository items;
 JpaManualContextStore(SpringManualContextItemJpaRepository items){this.items=items;}
 public void save(TaskId taskId, List<ManualContextItem> contextItems){
  int order=0;
  for (ManualContextItem item : contextItems) {
   ManualContextItemEntity e=new ManualContextItemEntity();
   e.id=UUID.randomUUID();e.taskId=taskId.value();e.name=item.name();e.content=item.content();e.itemOrder=order++;
   items.save(e);
  }
 }
 public List<ManualContextItem> findByTaskId(TaskId taskId){
  return items.findByTaskIdOrderByItemOrderAsc(taskId.value()).stream().map(e->new ManualContextItem(e.name,e.content)).toList();
 }
}
