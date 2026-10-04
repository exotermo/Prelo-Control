package br.com.exotermo.hermes.app.infrastructure.persistence;
import br.com.exotermo.hermes.app.application.ContextSnapshotRepository;
import br.com.exotermo.hermes.app.domain.*;
import java.util.*;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Transactional;

// Snapshots are append-only: resolve() (ContextResolver) always mints a fresh ContextSnapshotId,
// so save() is only ever called once per id. No optimistic locking needed here — there is no
// update path to race against.
@Repository public class JpaContextSnapshotStore implements ContextSnapshotRepository {
 private final SpringContextSnapshotJpaRepository snapshots;
 private final SpringContextSnapshotItemJpaRepository items;
 JpaContextSnapshotStore(SpringContextSnapshotJpaRepository snapshots, SpringContextSnapshotItemJpaRepository items){this.snapshots=snapshots;this.items=items;}

 @Transactional
 public ContextSnapshot save(ContextSnapshot snapshot){
  ContextSnapshotEntity entity=new ContextSnapshotEntity();
  entity.id=snapshot.id().value();entity.taskId=snapshot.taskId().value();entity.version=snapshot.version();entity.resolvedAt=snapshot.resolvedAt();
  snapshots.save(entity);
  for (ContextSnapshotItem item : snapshot.items()) {
   ContextSnapshotItemEntity itemEntity=new ContextSnapshotItemEntity();
   itemEntity.id=UUID.randomUUID();itemEntity.snapshotId=entity.id;itemEntity.name=item.name();itemEntity.content=item.content();
   itemEntity.sourceType=item.sourceType().name();itemEntity.provenance=item.provenance();itemEntity.itemOrder=item.order();
   items.save(itemEntity);
  }
  return snapshot;
 }

 public Optional<ContextSnapshot> findById(ContextSnapshotId id){return snapshots.findById(id.value()).map(this::toDomain);}

 public Optional<ContextSnapshot> findLatestByTaskId(TaskId taskId){
  return snapshots.findByTaskIdOrderByVersionDesc(taskId.value()).stream().findFirst().map(this::toDomain);
 }

 private ContextSnapshot toDomain(ContextSnapshotEntity entity){
  List<ContextSnapshotItem> domainItems = items.findBySnapshotIdOrderByItemOrderAsc(entity.id).stream()
      .map(i -> new ContextSnapshotItem(i.name, i.content, ContextSourceType.valueOf(i.sourceType), i.provenance, i.itemOrder))
      .toList();
  return new ContextSnapshot(new ContextSnapshotId(entity.id), new TaskId(entity.taskId), entity.version, entity.resolvedAt, domainItems);
 }
}
