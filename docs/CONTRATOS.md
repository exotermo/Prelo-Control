# Contratos do Prelo Control para outros sistemas

> Estado em 2026-10-04 (fases até T). Referência para quem integra com o Prelo: BastionDeploy, Work
> Control, scripts e o próprio messager. O Prelo é a **fonte de verdade** de projetos, tasks, execuções,
> pipeline, servidores e aprovações; ninguém mantém uma cópia própria dessas decisões.

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
