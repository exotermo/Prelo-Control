# Plano incremental de implementação

Este plano começa somente após aprovação da arquitetura e das decisões abertas de plataforma.

| Etapa | Entregável pequeno | Critério de aceitação |
|---|---|---|
| 0 | Bootstrap escolhido, formatter, lint e teste de fumaça | build e teste vazios passam em ambiente limpo |
| 1 | Tipos de domínio: IDs, Task, Context, Directive, GeneralAgent e Execution | fluxo create → execute → resultado coberto por teste de aplicação |
| 2 | Portas de repositório e implementação SQLite com migrações | criar/reabrir task e directive preserva dados e versões |
| 3 | Casos de uso de task + orquestração linear | task persiste, executa via porta LLM e registra execution sem HTTP no domínio |
| 4 | ContextResolver com fontes manuais e ContextSnapshot | somente itens selecionados entram no snapshot com proveniência |
| 5 | LanguageModel port + um adaptador configurável/mock | resposta estruturada, timeout e erro tipado testados sem provider real |
| 6 | GeneralAssistantAgent e TaskOrchestrator linear | task simples percorre READY → RUNNING → COMPLETED/FAILED e possui trace |
| 7 | ToolRegistry, ferramenta read-only de demonstração e PermissionPolicy | ✅ concluído em `hermes-go` (ver `CURRENT_STATE.md`) — ação bloqueada/permitida conforme perfil, toda chamada é auditada |
| 8 | Approval workflow | ✅ concluído em `hermes-go` (ver `CURRENT_STATE.md`) — risco moderado não executa antes de aprovação válida e expira corretamente |
| 9 | UI mínima de conversa/task em MVVM+MVI | intents, states, effects e loading/erro observáveis em testes |
| 10 | Logs estruturados, métricas locais e tela/detalhe de execução | trace correlaciona task, agent, modelo e ferramenta |
| 11 | Hardening e documentação operacional | redaction, configuração sem secrets no repositório e testes de regressão |

Fora da V1: pesquisa e oportunidades, scheduler/briefing, agentes de código, ExoDeploy, homelab, educação adaptativa, finanças, economia e knowledge graph. Cada um inicia como um ADR e um vertical slice independente, usando as portas do núcleo.

## Etapa 6.5 e reescrita em Go (ADR-012, ADR-013)

As etapas 0–6 acima foram implementadas em Java (`hermes-app/`). A etapa 6.5 (execução assíncrona durável) motivou a decisão de reescrever `hermes-app` inteiro em Go (`hermes-app-go/`), em paralelo, antes de construir a fila — ver ADR-012. A tabela abaixo é o plano incremental dessa reescrita, seguindo o mesmo espírito da tabela acima (fatias pequenas, testáveis antes de avançar).

| Etapa | Entregável pequeno | Critério de aceitação |
|---|---|---|
| G0 | Esqueleto `hermes-app-go/`, health check, Dockerfile, entrada em `compose.yaml` | container sobe e healthcheck verde ao lado de `hermes`/`llm-gateway` |
| G1 | Domínio Go (Task/Execution/AgentDefinition/ContextSnapshot) com transições guardadas | testes unitários cobrem toda transição válida/inválida |
| G2 | Migração `execution_jobs` (goose, V7) | aplica ao lado de V1–V6 (Flyway) sem conflito |
| G3 | Repositórios Task/Execution com claim otimista versionado | teste de concorrência: exatamente um vencedor |
| G4 | Cliente Gateway + JWT HS256 | paridade de contrato com o Java (headers, claims, timeouts, erros) |
| G5 | ContextResolver + repositórios de contexto | paridade de ordem/limites com o Java |
| G6 | API: criar/consultar task, passthrough de chat | contrato bate com `API.md` |
| G7 | `/execute` síncrono temporário | paridade de resultado com o `hermes` Java |
| G8 | Tabela/repo `execution_jobs`, claim atômico | teste de concorrência no claim do job |
| G9 | Redis Streams + worker pool, `/execute` vira `202 Accepted` | fluxo completo: POST → 202 → processamento assíncrono → COMPLETED |
| G10 | `GET /tasks/{id}/executions/{id}` | cobre todos os status + 404 |
| G11 | Sweeper: recuperação de jobs órfãos, retry/backoff, DEAD | teste de crash: reprocessa exatamente uma vez |
| G12 | Documentação + checklist de paridade lado a lado | checkpoint de aprovação do usuário antes de qualquer cutover ou remoção do módulo Java |

Estado de execução desta tabela: ver `CURRENT_STATE.md`.
