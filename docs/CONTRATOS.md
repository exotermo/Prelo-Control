# Contratos do Prelo Control para outros sistemas — v2

> Estado em 2026-10-04 (fases até T + Fase 0 de integração). Referência para quem integra com o Prelo:
> BastionDeploy, Work Control, scripts e o próprio messager. O Prelo é a **fonte de verdade** de identidade,
> projetos, tasks, execuções, pipeline, servidores e aprovações; ninguém mantém uma cópia própria dessas
> decisões (ADR-015). Seções 1–5 descrevem o que **já existe**; a seção 6 lista o que está **congelado e ainda
> não implementado**. Mudanças: `docs/integracoes/PROPOSTAS.md`.

## 0. Quem é dono de quê

| Tema | Prelo | Work Control | BastionDeploy |
|---|---|---|---|
| Identidade, login, TOTP, sessões, revogação | **dono** | consome (biometria só local) | usa chave de API do projeto |
| Workspace, projetos, membros, papéis | **dono** | exibe | referencia `projectId` |
| Clientes (CRM) | **dono** | exibe | — |
| Tasks, execuções, pipeline, agentes, ferramentas | **dono** | exibe, cria, delega | — |
| Política de risco e aprovações | **dono (única)** | decide pela API do Prelo | **pede** e obedece |
| Aviso ao dono (WhatsApp) | **dono** | — | — |
| Servidores monitorados (SSH, saúde) | **dono** | exibe | — |
| Apps, ambientes, repositórios, segredos de app | referência | exibe via Prelo | **dono** |
| Pedidos de deploy, jobs, releases, rollback, logs de build | espelho do status | exibe via Prelo | **dono** |
| Workers, isolamento, publicação (cloudflared) | — | exibe URL | **dono** |
| Auditoria | decisões | — | execução, reportada ao Prelo |

## 1. Autenticação

Todo `/api/v1/*` exige `Authorization: Bearer <token>`. Três tipos de credencial:

| Credencial | Para quem | Como funciona |
|---|---|---|
| Sessão do dashboard (`token_use=dashboard`) | pessoas (ADMIN/OPERATOR) | login + TOTP; access token de 15 min, refresh por cookie HttpOnly |
| Chave de API do projeto (`prl_live_…`) | integrações de um projeto | criada em Projetos → Integrações; presa a **um** projeto; escopos `tasks:create`, `tasks:read`, `tasks:execute`, `observability:read` (nunca decide aprovação) |
| Token técnico (`token_use=technical`) | serviços internos (bridge) | JWT HS256 com o segredo compartilhado `PRELO_API_JWT_SECRET`; escopos mínimos por chamada |

Escopos relevantes: `projects:read|manage`, `tasks:create|read|execute`, `approvals:read|decide`,
`observability:read`, `servers:read|manage`, `clients:read|manage|delete`, `integrations:manage`,
`settings:manage`, `users:manage`, `approvals:decide-owner` (só o bridge — resposta do dono no WhatsApp).

**Projeto atual:** rotas de tasks, servidores, pipeline, aprovações e integrações leem o cabeçalho
`X-Project-Id`. Sem ele = balde "sem projeto". OPERATOR só acessa projetos dos quais é membro (403);
chave de API só o próprio projeto (403 em outro).

## 2. Recursos

### Project
`GET/POST /api/v1/projects`, `PATCH|DELETE /api/v1/projects/{id}`, `PUT /api/v1/projects/{id}/client`.
Campos: `id, name, description, createdAt, memberCount, defaultAgentId, instructions, coverColor, clientId`.

### Client (CRM)
`GET/POST /api/v1/clients`, `GET|PATCH|DELETE /api/v1/clients/{id}`, `POST|DELETE …/contacts`,
`GET …/timeline`. Status `LEAD|ACTIVE|INACTIVE|DISCARDED`. Contato `PHONE|WHATSAPP|EMAIL`; telefone em
E.164; WhatsApp aceita telefone ou ID `…@lid`. Um contato pertence a um único cliente.

### Task
`POST /api/v1/tasks` `{description, agentId?, context?, source?, contactAddress?}` → 201.
`GET /api/v1/tasks`, `GET /api/v1/tasks/{id}`, `POST /api/v1/tasks/{id}/execute` → `{executionId}` (assíncrono).
Campos: `id, description, status (CREATED|QUEUED|RUNNING|COMPLETED|FAILED), agentId, createdAt,
source (MANUAL|MESSAGING), projectId, clientId, contactAddress`.
`contactAddress` só é aceito com `source=MESSAGING` e de token técnico (bridge).

### Execution
`GET /api/v1/tasks/{taskId}/executions/{executionId}` → `executionId, taskId, agentId, status
(PENDING|RUNNING|COMPLETED|FAILED), result, error, provider, model, startedAt, completedAt`.
`provider`/`model` dizem quem respondeu de fato (ex.: `codex_cli · gpt-6.1-sol`).
Turnos: `GET …/executions/{id}/turns` (LLM_CALL / TOOL_CALL, com entrada e saída). Árvore de delegação:
`GET /api/v1/tasks/{id}/tree`.

### Pipeline
`GET /api/v1/pipeline` → árvores de tasks ativas com `pipelineStatus`
`CREATED|QUEUED|RUNNING|AWAITING_APPROVAL|AWAITING_SUBTASK|COMPLETED|FAILED`.

### Server
`GET /api/v1/servers`, `POST` (cadastro com chave SSH, testada antes de salvar), `GET|DELETE /{id}`,
`POST /{id}/health-check`. A chave privada nunca sai da API.

### Approval
`GET /api/v1/approvals` (pendentes do projeto), `GET /{id}`, `POST /{id}/approve|deny` `{decidedBy}`.
Campos: `id, toolCallId, scope (texto do que vai rodar + impacto), status
(PENDING|APPROVED|DENIED|EXPIRED), requestedAt, expiresAt (30 min), decidedAt, decidedBy, shortCode`.
Decisão pelo dono no WhatsApp: `POST /api/v1/approvals/by-code/{code}/{approve|deny}` — só escopo
`approvals:decide-owner`. Uma aprovação é de uso único; expirar ou negar não executa nada.

## 3. Ferramentas dos agentes e risco

A ferramenta pedida pelo modelo passa pelo `PermissionPolicy`: LOW roda direto; MODERATE/HIGH vira
aprovação (dashboard + WhatsApp do dono). O agente nunca decide a própria autorização.

| Ferramenta | Risco |
|---|---|
| current_time, search_workspace, get_client, list_servers, check_server_health, inspect_website | LOW |
| echo, delegate_to_agent, add_client_note | MODERATE |
| send_whatsapp_message | HIGH |

Integrações novas (ex.: "pedir deploy ao BastionDeploy") entram como **ferramenta** com risco e
impacto declarados — nunca como chamada direta que pule a aprovação.

## 4. Webhooks de saída

Configurados por projeto (Integrações). `POST` JSON com cabeçalhos `X-Prelo-Event`, `X-Prelo-Delivery`,
`X-Prelo-Timestamp`, `X-Prelo-Signature: sha256=HMAC(segredo, timestamp + "." + corpo)`.
Eventos: `task.completed`, `task.failed`, `approval.pending`, `server.offline`, `webhook.test`.
Entrega com retry exponencial (até 8 tentativas); destino interno bloqueado (anti-SSRF).

## 5. Modelos (llm-gateway)

Ninguém além do gateway fala com provedores. O core manda `metadata.origin` (= `task.source`):
conexões por assinatura (`claude_cli`, `codex_cli`) só atendem `MANUAL`; WhatsApp e prospecção usam a
conexão por API da instância (ou o modelo simulado).

## 6. Contratos congelados, ainda não implementados

| # | Lacuna | Contrato | PR do Prelo |
|---|---|---|---|
| G1 | Workspace | `workspaceId` da instância — `integracoes/sessao-mobile.md` | PR-1 |
| G2 | Sessão do app (refresh no corpo, por dispositivo, rotação, revogação) | `integracoes/sessao-mobile.md` | PR-2 |
| G3 | `GET /api/v1/me` | `integracoes/sessao-mobile.md` | PR-1 |
| G4 | Tempo real | v1 polling; v2 SSE `GET /api/v1/events/stream` (a especificar) | PR-4 |
| G5 | Pedido de ação externa | `integracoes/action-requests.md` | PR-3 |
| G6 | Decisão de volta (`action.decided` + `GET`) | `integracoes/action-requests.md` | PR-3 |
| G7 | Resultado da execução | `integracoes/action-requests.md` | PR-3 |
| G8 | Ações/deploys por projeto (leitura) | `integracoes/action-requests.md` | PR-3 |
| G9 | Confirmação com TOTP para aprovar ALTO pelo app | `integracoes/sessao-mobile.md` | PR-2 |
| G10 | Ferramenta `request_deploy` (agente pede; aprovação é a do G5) | a especificar com a API do Bastion | PR-4 |
| G11 | Push no celular (sem dados sensíveis) | a especificar | PR-4 |

Enquanto não existem: o Work Control usa o login atual com sessão só em memória e polling; o BastionDeploy
desenvolve contra um stub de `action-requests`.
