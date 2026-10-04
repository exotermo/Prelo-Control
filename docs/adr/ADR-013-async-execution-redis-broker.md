# ADR-013 — Execução assíncrona durável (etapa 6.5): Postgres como fonte de verdade, Redis como broker de despacho

**Status:** Aceito

## Contexto

Execuções de task bloqueavam a requisição HTTP (`POST /tasks/{id}/execute` síncrono), impedindo um canal conversacional confiável e não sobrevivendo a um restart do processo no meio de uma execução. A etapa 6.5 exige: fila persistida, worker, resposta `202 Accepted`, consulta de execution, retry e recuperação após restart — com garantias de idempotência ponta a ponta.

## Decisão

- **Postgres é a fonte de verdade durável.** Nova tabela `prelo_app.execution_jobs` (migration goose V7), 1:1 com `task_executions` via `execution_id` único. Estados: `PENDING, CLAIMED, RUNNING, DONE, FAILED, RETRY, DEAD`.
- **`execution_jobs.id == task_executions.id == Execution.id`**, e esse mesmo UUID é o `requestId`/`X-Request-Id` enviado ao `llm-gateway` — chave de idempotência única em toda a pipeline.
- **Claim atômico, não lock otimista clássico.** Diferente de `Task`/`Execution` (que usam `UPDATE ... WHERE version=$1`, replicando o padrão exato do Java), o claim de job usa `UPDATE execution_jobs SET status='CLAIMED' ... WHERE id=$1 AND status IN ('PENDING','RETRY')` — não há um valor de versão pré-lido pelo chamador para comparar, então a corrida é resolvida inteiramente pelo lock de linha do Postgres durante o UPDATE.
- **Redis é só despacho, nunca durabilidade.** Um Redis Stream (`prelo:execution-jobs`, grupo `prelo-workers`) recebe um `XADD` best-effort após o commit da transação Postgres. Workers fazem `XReadGroup`, mas nunca confiam no payload da mensagem — sempre releem o job do Postgres e tentam o claim atômico; `XAck` acontece logo após a leitura, porque o papel do Redis aqui é "avisar", não "seguar trabalho". Se o Redis cair, for resetado, ou uma mensagem se perder antes de ser lida, nenhum job se perde: o sweeper descobre pelo Postgres sozinho.
- **Sweeper independente do Redis** (`internal/infrastructure/worker/sweeper.go`), rodando a cada poucos segundos: (1) move jobs `CLAIMED`/`RUNNING` com `lease_expires_at` vencido para `RETRY` (com backoff) ou `DEAD` (se `max_attempts` esgotado); (2) processa diretamente qualquer job `PENDING`/`RETRY` já disponível. Isso cobre tanto a recuperação após restart quanto o fallback caso o Redis esteja totalmente ausente — o mesmo código atende os dois casos.
- **Idempotência end-to-end**: antes de chamar o Gateway, o worker relê a `Execution`; se já `COMPLETED`/`FAILED`, encerra sem rechamar o Gateway. Isso cobre o caso de crash entre "Gateway respondeu" e "job marcado DONE". `lease_expires_at` fica folgado (2 minutos) acima do timeout do Gateway (10s, ADR sobre o Gateway), garantindo que qualquer retry só aconteça depois que a tentativa anterior já teria expirado de qualquer forma.
- **Contrato HTTP**: `POST /tasks/{id}/execute` retorna `202 Accepted` imediatamente com `{executionId, taskId, status}`; a orquestração real acontece no worker. `GET /tasks/{id}/executions/{executionId}` expõe o estado corrente (`PENDING/RUNNING/COMPLETED/FAILED`, resultado, erro, timestamps) para polling.
- `Task.status = QUEUED` só é alcançável a partir de `CREATED` (`Task.Queued()`), o que torna um segundo `POST .../execute` concorrente na mesma task naturalmente rejeitado, sem lógica extra de deduplicação na API.

## Alternativas

- Usar apenas o Redis Streams Pending Entries List (PEL) para redelivery: descartado — criaria dois mecanismos de redelivery sobrepostos (PEL do Redis + lease do Postgres); manter só o do Postgres é mais simples e não depende da durabilidade do Redis.
- `SELECT ... FOR UPDATE SKIP LOCKED` no `ListClaimable` do sweeper: avaliado, mas não necessário para corretude — o claim por linha já é atômico; duas leituras concorrentes do mesmo id só custam uma tentativa de claim perdida, nunca um processamento duplicado. Mantido como possível otimização futura de contenção, não implementado agora.

## Consequências

- `execution_jobs` é metadado de agendamento, separado de `task_executions` (que continua sendo o registro de auditoria/resultado, sem mudança de schema).
- Um worker morto no meio de um job deixa o job `CLAIMED`/`RUNNING` até o próximo tick do sweeper — não há redelivery instantânea sem o sweeper.
- Design pensado para reuso: um futuro tipo de job (ex.: mensagens de canal) poderia reutilizar a mesma infraestrutura de worker/sweeper — não desenhado agora, e sem relação com o futuro serviço de mensageria (que é um serviço Java 21 + OAuth2 separado, per ADR-012).

## Implicações de segurança

Redis roda só na rede `prelo_internal` (ADR-008), sem porta publicada e sem egress. Nenhum dado sensível (prompt, resposta, segredo) trafega pelo Redis — só o UUID do job.

## Evolução futura

Cutover de `compose.yaml` e remoção de `legacy-java-app/` (Java) só após checklist de paridade (ver `CURRENT_STATE.md`), com confirmação explícita do usuário.
