# Hermes Agent — Arquitetura proposta

## 1. Estado encontrado (discovery)

Em 16 de setembro de 2026, o diretório `projects/hermes e renato` está vazio e não possui repositório Git. Não há código, build system, framework, módulos, dependências, banco de dados, infraestrutura, entry points, testes ou documentação preexistentes. Esta proposta é, portanto, uma fundação para um **monólito modular local**, e não uma refatoração.

Antes de iniciar a implementação, ainda será necessária uma decisão explícita sobre plataforma de apresentação (por exemplo, Android/Kotlin, desktop ou web). O núcleo abaixo não depende dessa decisão.

## 2. Visão e limites da V1

Hermes é o orquestrador: recebe uma intenção, constrói uma tarefa, resolve contexto e diretivas, escolhe agentes, aplica permissões, acompanha a execução e consolida um resultado auditável. Ele não concentra a lógica de todos os domínios nem executa ações de risco sem aprovação.

A V1 valida o núcleo com conversa, tarefa, contexto, diretivas, um agente básico, ferramentas inicialmente somente de leitura, aprovação e rastreabilidade. Research, oportunidade, agenda, educação, finanças, economia, ExoDeploy e homelab entram como módulos posteriores atrás das mesmas portas do núcleo.

## 3. Estrutura modular proposta

```text
presentation/                         # adaptadores de interface; ainda sem tecnologia definida
  conversation/                       # View, ViewModel, intents, state, effects
  task/

application/                          # casos de uso e coordenação de fluxos
  conversation/
  task/                               # CreateTask, StartTask, CancelTask, ApproveAction
  orchestration/                      # TaskOrchestrator, agent selection, result consolidation
  context/                            # ContextResolver e prompt/context compiler

domain/                               # regras, entidades, value objects e portas
  core/                               # Project, Goal, Directive, Event, Result
  conversation/                       # Conversation, Message
  task/                               # Task, TaskRun, Workflow, status
  agent/                              # AgentDefinition, AgentRun, Capability
  context/                            # ContextItem, ContextSnapshot, ContextPolicy
  memory/                             # Memory, MemoryScope, retention policy
  tool/                               # ToolDefinition, ToolInvocation, ToolResult
  permission/                         # Permission, RiskLevel, Approval
  observability/                      # ExecutionTrace, audit event
  knowledge/                          # futuro: knowledge graph compartilhado
  research/ opportunity/ education/ finance/ economics/  # futuros bounded contexts

infrastructure/                       # implementações das portas
  llm/                                # provider adapters
  persistence/                        # PostgreSQL local na topologia inicial
  tools/                              # filesystem, git, web, ExoDeploy etc.
  observability/                      # logs estruturados e métricas
  security/                           # secret provider, redaction
```

As dependências fluem para dentro: `presentation → application → domain`; `infrastructure` implementa portas declaradas no domínio/aplicação e é conectada no composition root. Nenhuma entidade de domínio conhece UI, banco, SDK de LLM ou shell.

## 4. Diagrama de módulos e fluxo de dados

```text
UI / API / CLI
       │ Intent
       ▼
Presentation (MVI state/effects; MVVM boundary)
       │ use case
       ▼
Application ── TaskOrchestrator ── ContextResolver
       │               │                   │
       ▼               ▼                   ▼
Domain: Task ───── AgentDefinition ─ ContextSnapshot + Directives
       │               │                   │
       └──── PermissionPolicy ─ ToolInvocation / Approval ──┐
                                                              ▼
Infrastructure adapters: LLM | SQLite | Filesystem | Git | Web | ExoDeploy
                                                              │
                                                              ▼
                                      ExecutionTrace → result → consolidated response
```

## 5. Modelo de domínio

O núcleo começa com agregados pequenos e IDs estáveis:

- `Project`: escopo de trabalho; contém metas, diretivas e referências de contexto, não listas carregadas integralmente.
- `Goal`: objetivo mensurável ou qualitativo; pode originar tarefas e, no futuro, oportunidades e planos de estudo.
- `Conversation` e `Message`: registro da conversa. Mensagens são fonte de contexto, não memória automática.
- `Task`: unidade de trabalho solicitada; possui intenção, estado, critérios de aceitação, contexto congelado, diretivas aplicadas, plano e resultado.
- `Workflow`: receita reutilizável de etapas/agentes. Uma task pode executar um workflow; a V1 pode ter fluxo linear fixo.
- `AgentDefinition`: identidade, capacidades, requisitos de contexto, ferramentas permitidas, diretivas e contrato de saída.
- `AgentRun` / `TaskRun`: execução imutavelmente rastreável, com estado, tempos, custo e referências a eventos.
- `Directive`: regra persistente e versionada, com escopo (`global`, `project`, `agent`, `task`) e precedência explícita.
- `ContextItem` e `ContextSnapshot`: uma fonte relevante e a seleção imutável usada pela execução.
- `Memory`: conhecimento persistente curado, com proveniência, confiança, escopo, expiração e política de revisão.
- `ToolDefinition`, `ToolInvocation`, `ToolResult`: contrato de ferramenta, pedido de execução e resultado sanitizado.
- `Approval`: decisão do usuário sobre uma operação concreta, de duração e escopo limitados.
- `ExecutionEvent` / `ExecutionTrace`: fatos de auditoria correlacionados por task, run e invocation.

Domínios futuros (research, opportunity, education, finance e economics) publicam dados próprios e se conectam a `Goal`, `Knowledge` e `Task` por IDs e eventos de aplicação, sem crescer o agregado `Task`.

## 6. Modelo de agente

`AgentDefinition` é dado/configuração de domínio, não uma classe de UI. Seu contrato mínimo é:

```text
id, name, role, capabilities, allowedTools, permission ceiling,
model profile, context requirements, directive scopes, output schema,
validation policy
```

O `TaskOrchestrator` seleciona agentes por capacidade e política, não por strings soltas em telas. Na V1, um `GeneralAssistantAgent` pode produzir uma resposta estruturada; o próximo passo acrescenta `PlanningAgent`, seguido de agentes de execução e validação. Cada `AgentRun` recebe um contexto congelado e retorna saída validada pelo seu schema.

## 7. Contexto e memória

Contexto é efêmero, específico à tarefa e versionado por snapshot: arquivos, trechos, logs, mensagens relevantes, estado de projeto e resultados de ferramentas. `ContextResolver` seleciona somente fontes necessárias, aplica limites de tamanho/orçamento e registra a proveniência de cada item.

Memória é conhecimento persistente e curado: preferências, decisões, objetivos, convenções e conhecimentos consolidados. Uma memória só é criada ou alterada por caso de uso explícito e com proveniência; conversas e contextos não são promovidos automaticamente. O prompt recebe referências e resumos selecionados, nunca o banco inteiro de memória.

No futuro, `Knowledge` poderá representar tópicos e relações para educação, física, matemática, economia, projetos e oportunidades. Ele é um bounded context separado de memória operacional.

## 8. Ferramentas e permissões

Ferramentas são adaptadores registrados por `ToolRegistry`, descritos por schemas de entrada/saída, permissões requeridas, risco e política de execução. `ExecutionEngine` valida entrada, resolve autorização, solicita aprovação quando obrigatória, executa por adaptador e registra resultado redigido.

```text
READ_ONLY       executar e auditar
LOW_RISK        executar conforme escopo pré-autorizado
MODERATE_RISK   confirmação explícita por invocação ou política curta
HIGH_RISK       explicação, escopo concreto e confirmação explícita
DESTRUCTIVE     confirmação explícita, alvo validado e trilha de auditoria reforçada
```

Uma aprovação é vinculada à ação normalizada, alvo, parâmetros relevantes e prazo. Ela não autoriza comandos diferentes nem evita a checagem de escopo. Secrets permanecem em um `SecretProvider`; logs, prompts e respostas usam redaction.

## 9. MVVM + MVI

MVVM delimita responsabilidades: a View renderiza, a ViewModel adapta estado e invoca casos de uso; regras de negócio vivem no domínio e na aplicação. MVI oferece fluxo unidirecional dentro de cada feature:

```text
User → Intent → ViewModel → Use case → state reduction → State → View
                                      └→ one-shot Effect → View
```

Exemplo de conversa: `SendMessage` cria ou atualiza `Conversation`, chama `CreateTask`/`StartTask`, recebe atualizações de execução e reduz `ChatState`. Navegação, alertas de aprovação e erros recuperáveis são `Effect`; estado atual, histórico paginado e execução ativa são `State`. ViewModel não seleciona agentes, não monta prompts e não chama SDKs diretamente.

## 10. Execução de uma task

```text
1. Intenção do usuário cria Task em DRAFT.
2. Caso de uso valida escopo, diretivas e critérios; Task vira READY.
3. Orchestrator cria TaskRun e resolve agente(s) elegíveis.
4. ContextResolver cria ContextSnapshot com fontes, memória relevante e proveniência.
5. Prompt compiler combina role, diretivas, tarefa, contexto limitado, ferramentas e schema de saída.
6. AgentRun produz plano/resposta ou solicita ToolInvocation.
7. PermissionPolicy permite, bloqueia ou cria ApprovalRequested.
8. ExecutionEngine executa somente após a política aplicável e grava ToolResult redigido.
9. Orchestrator valida schema/critério, consolida resultado e fecha TaskRun como COMPLETED, FAILED ou CANCELLED.
10. Eventos e trace permitem à UI mostrar progresso e ao usuário revisar decisões.
```

Uma execução não cria memória sozinha. Uma sugestão de memória pode ser apresentada para confirmação ou tratada por política futura claramente visível.

## 11. Persistência

V1: PostgreSQL único em rede Docker isolada, via uma porta de repositório por agregado, com migrações versionadas. A escolha substitui a hipótese inicial de SQLite porque a topologia desta etapa já exige banco de serviço compartilhado e persistência operacional. Arquivos grandes, logs brutos e anexos ficam em armazenamento de arquivos com metadados e hash na base.

Persistir desde o início: projects, goals, conversations/messages, tasks/task_runs, agent definitions/runs, directives, memory, context snapshots e itens de proveniência, approvals, tool invocations/results sanitizados e execution events. Não persistir prompts completos ou conteúdo sensível por padrão; isso deve ser uma opção auditável. Quando a escala ou sincronização exigir, PostgreSQL pode substituir o adaptador sem alterar o domínio.

## 12. Abstração de LLM

A porta `LanguageModel` recebe um `CompiledPrompt`/mensagens estruturadas, configuração de modelo e schema de resposta; retorna `ModelResponse` com conteúdo, uso, modelo, latency e falha tipada. Adaptadores de OpenAI, modelo local e outros providers ficam na infraestrutura. Tool calling é convertido para `ToolInvocation` do domínio; nunca executado diretamente pelo provider. Retry, timeout, orçamento e fallback são políticas de aplicação, observáveis por run.

## 13. Observabilidade

Cada tarefa gera `traceId`; cada run, chamada de modelo e ferramenta possui ID correlacionado. Eventos mínimos: task criada/iniciada/concluída/falhou, contexto resolvido, agent iniciado/finalizado, aprovação solicitada/decidida, ferramenta solicitada/finalizada e falha. Logs são estruturados e redigidos; métricas incluem duração, tokens, custo, erros e taxa de aprovação. A V1 usa logs locais e tabelas de eventos; exportação para um backend de observabilidade é adaptador futuro.

## 14. Segurança

- princípio de menor privilégio por agente e ferramenta;
- autorização central antes de qualquer execução externa;
- confirmação explícita para riscos moderados ou superiores;
- validação de schemas, alvos e limites antes de executar;
- secret provider e redaction obrigatória em traces;
- separação de ambientes e configurações; sem credenciais no repositório;
- auditoria imutável de decisões e ações importantes;
- resultado declarado somente com evidência de execução; falhas são explícitas.

## 15. Roadmap

**V1 — núcleo local validável.** Conversa, tasks, diretivas, contexto por snapshot, agente geral/planejador, LLM adapter, tool registry read-only, aprovação, SQLite, logs e UI mínima.

**V2 — especialização e automação assistida.** Research e opportunity com fontes configuráveis e evidência; workflows de coding/review/test; agendamento de briefing; conectores GitHub/feeds/ExoDeploy; notificações; primeiros módulos de educação e finanças como acompanhamento e cálculo, sem aconselhamento decisório.

**V3 — ecossistema pessoal.** Knowledge graph curado, aprendizagem adaptativa, análises econômicas com fontes e distinção fato/análise, integrações de homelab por políticas, automações event-driven, sincronização/backup e observabilidade ampliada.

## 16. Riscos e decisões em aberto

Não há base para decidir ainda a plataforma de UI, linguagem, biblioteca de persistência, provedor de LLM ou estratégia de identidade/sincronização. Essas escolhas devem ser fechadas em ADRs curtos antes do primeiro módulo executável. A proposta evita microserviços, filas distribuídas, múltiplos bancos e grafo dedicado na V1.
