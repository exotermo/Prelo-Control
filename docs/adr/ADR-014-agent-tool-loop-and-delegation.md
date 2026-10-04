# ADR-014 — Loop de tool-calling, orquestração multi-agente e observabilidade de execução

**Status:** Aceito

## Contexto

As etapas 7/8 (ADR-004) deram um *gate* de permissão e um registro de auditoria por chamada de
ferramenta — mas só via invocação explícita por API, nunca acionado pelo próprio LLM durante uma
execução. O Prelo precisava de três coisas, pedidas juntas por exigirem a mesma base:

1. **Loop de tool-calling automático** — o LLM decide chamar uma ferramenta durante a execução.
2. **Orquestrador multi-agente** — um agente delega trabalho a outro.
3. **Observabilidade de execução** — rastreabilidade completa, com ênfase explícita em
   idempotência: nenhuma chamada externa (Gateway ou ferramenta) pode repetir um efeito colateral
   já ocorrido se um job for reprocessado após um crash.

A ativação do provider real de LLM ficou deliberadamente fora de escopo — todo o trabalho abaixo
é validado contra o `MockProvider` (Java, `llm-gateway`), estendido para simular `TOOL_USE`.

## Decisão

- **Ledger apend-only por execução** (`execution_turns`, migration goose V9): um turno por
  chamada ao Gateway (`LLM_CALL`) ou por chamada de ferramenta (`TOOL_CALL`/`SUBTASK`), sempre
  gravado *antes* de qualquer chamada externa acontecer — nunca depois. `RequestID` de cada turno
  é determinístico (`domain.TurnRequestID(executionID, turnNumber) = "<executionID>-turn-<N>"`),
  não um UUID aleatório por tentativa — é essa determinação, mais a unique index em
  `(execution_id, turn_number)` e em `request_id`, que garante que um job reprocessado pelo
  sweeper nunca duplica uma chamada real: `RunAgentLoopUseCase.Run` sempre reconstrói o histórico
  de mensagens a partir do ledger antes de decidir qual é o próximo turno a rodar.
- **Suspender/retomar como mecanismo único** (`execution_suspensions`, mesma migration V9,
  `ExecutionJob` ganha o status `AWAITING_RESUME`): tanto aguardar aprovação humana (etapa 8)
  quanto aguardar uma sub-task delegada terminar (Fase C) usam a mesma estrutura —
  `{ExecutionID, Reason (APPROVAL|SUBTASK), ResumeKey}`. Só quem resolve o `ResumeKey` (aprovação
  decidida, ou sub-task concluída) pode devolver o job para `PENDING`. `AWAITING_RESUME` não tem
  lease — o `Sweeper` nunca o trata como órfão porque sua cláusula de recuperação é uma lista
  explícita (`status IN ('CLAIMED','RUNNING')`), não uma exclusão; não foi necessário nenhum
  código novo no sweeper para essa garantia, só um teste que trava a invariante
  (`TestSweeper_NeverTouchesAnAwaitingResumeJob`).
- **Contrato Gateway ganha `tools`/`TOOL_USE`** (cross-repo, `llm-gateway` + `prelo-core`):
  `LLMRequest.tools` (nome+descrição, sem JSON schema de args — deliberadamente mínimo) e
  `LLMResponse.kind` (`FINAL`|`TOOL_USE`, com `content` ou `toolName`/`toolArgsJson` conforme o
  caso). `MockProvider.java` fica determinístico: oferece a primeira tool se ainda não viu um
  resultado de ferramenta na conversa (convenção de mensagem `[tool_result:...]`, só usada em
  modo mock), senão responde `FINAL`. O `AnthropicProvider` real não foi tocado — continua
  sempre `FINAL`, documentado como pendência para quando o provider real for ligado.
- **`RunAgentLoopUseCase`** é o novo dono de toda chamada ao Gateway (`ProcessJobUseCase` só
  claim/lease/transições de Task e finalização do que o loop devolve). Loop limitado a 8 chamadas
  ao Gateway por execução (`maxLLMCalls`) — trava de segurança contra um provider que nunca para
  de pedir ferramentas.
- **`delegate_to_agent`** (Fase C) é uma ferramenta como qualquer outra — catalogada, MODERATE
  por padrão (então normalmente passa por aprovação humana, igual qualquer ação de risco),
  auditada em `tool_calls` — mas sua execução não devolve um resultado inline: cria e enfileira
  uma Task filha real (mesmo pipeline completo, incluindo seu próprio loop) e suspende o pai
  (`SuspensionSubtask`, `ResumeKey = childTaskID`). `domain.NewSubtask` é o único construtor que
  produz uma Task com `ParentTaskID` setado, e é ali — não na ferramenta, não no loop — que
  `MaxDelegationDepth` (3 níveis) é imposto.
- **Hook de conclusão** (`ProcessJobUseCase.resolveParentSubtaskSuspension`, chamado em todo
  caminho terminal — sucesso, `terminalFailure`, `failExecution`): depois que QUALQUER execução
  termina, checa se existe uma suspensão `SUBTASK` esperando aquele `TaskID`; se sim, completa o
  turno aberto do pai com o resultado do filho e devolve o job dele para `PENDING`. Não existe
  polling do pai esperando o filho — é o filho terminando que acorda o pai, sempre.
- **API somente-leitura** (Fase D): `GET /tasks/{id}/executions/{id}/turns` (ledger completo,
  incluindo turnos ainda abertos numa execução `AWAITING_RESUME`) e `GET /tasks/{id}/tree`
  (árvore de delegação recursiva, cada nó com seu próprio status de execução). Nenhuma escrita
  nova — é leitura pura sobre o que as Fases A-C já persistem.
- **Logs estruturados**: toda transição relevante do loop (turno iniciado, decisão de ferramenta,
  suspensão, retomada) loga `execution=<id> turn=<n>` com o prefixo `agent-loop:`, grepável sem
  precisar abrir a UI.

## Alternativas

- **UUID aleatório por tentativa de turno**, com deduplicação por conteúdo: descartado — exigiria
  comparar mensagens para saber se duas tentativas são "a mesma", frágil. Um `RequestID`
  determinístico por `(executionID, turnNumber)` é comparação de string exata, sem ambiguidade.
- **Delegação como chamada síncrona bloqueante** (o tool `Execute` esperaria o filho terminar
  antes de retornar): descartado — prenderia um worker do pool por tempo indefinido, e um filho
  travado travaria o worker junto. Suspender o pai (liberando o worker imediatamente) e retomar
  via hook assíncrono foi a escolha, reaproveitando a mesma peça que a aprovação já usava.
- **Um "orquestrador" como serviço/processo separado**: descartado por escopo — delegação é só
  "mais uma Task no mesmo pipeline", não precisa de infraestrutura nova.

## Consequências

- `ProcessJobUseCase` agora distingue explicitamente primeira-entrada de retomada (não
  re-transiciona `Task`/`Execution` que já estão `RUNNING`, reusa o `ContextSnapshot` já
  resolvido) — um pouco mais de ramificação nesse caso de uso, mas nenhuma duplicação de lógica
  com `RunAgentLoopUseCase`.
- `ToolExecutor.Execute` ganhou um parâmetro `domain.Execution` (antes só `argsJSON`) — toda
  ferramenta existente (`current_time`, `echo`) precisou de uma alteração de assinatura trivial;
  só `delegate_to_agent` de fato usa o parâmetro novo (para achar a Task pai).
- `DecideApprovalUseCase` agora precisa saber diferenciar "aprovação de uma chamada normal"
  (retoma o job) de "aprovação de uma delegação" (troca a suspensão de `APPROVAL` para
  `SUBTASK`, não retoma ainda) — verificado pelo nome da ferramenta (`domain.DelegateToolName`),
  não por um tipo/interface separado; simples o bastante para não justificar mais abstração agora.
- Sem sweeper de expiração proativo para `ApprovalRequest` — expiração continua calculada na
  leitura (`EffectiveStatus`), não empurrada; uma aprovação esquecida só "expira" quando alguém
  olha para ela. Aceito desde a etapa 8, não revisitado aqui.

## Implicações de segurança

`delegate_to_agent` não ganha nenhum atalho de autorização — passa pelo mesmo `PermissionPolicy`
de qualquer ferramenta, e por padrão sempre pede aprovação humana antes de criar uma Task filha.
A trava de profundidade (`MaxDelegationDepth`) impede uma cadeia de delegação sem fim mesmo que
um operador aprove delegações repetidamente. Nenhum dado sensível novo entra em log — os
`agent-loop:` só carregam IDs e nomes de ferramenta, nunca conteúdo de mensagem.

## Evolução futura

O contrato Gateway já está pronto para tool-calling estruturado real; falta só o
`AnthropicProvider.java` passar `tools`/`tool_use`/`tool_result` no formato que a API da
Anthropic exige, quando o provider real for ligado (fora de escopo desta ADR). A UI própria do
Prelo (consumindo os dois endpoints novos de observabilidade) é a próxima fase, num repositório
separado (`prelo-dashboard`), ainda não iniciada.
