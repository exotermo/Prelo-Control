# API

`prelo-core` (Go, `127.0.0.1:8082`, execução assíncrona per ADR-013) é o serviço de produção desde o cutover de 2026-09-23 (ver `CURRENT_STATE.md`). `prelo` (Java, síncrono) foi parqueado — código intacto em `legacy-java-app/`, fora do `docker compose up` padrão — e é citado abaixo só onde seu contrato histórico diverge do de `prelo-core`, para quem precisar comparar.

Desde o hardening, todas as rotas `/api/v1/**` do prelo-core exigem `Authorization: Bearer` com
JWT emitido pelo messaging-core. O token deve conter `iss`, `aud`, `exp`, `tenant_id`,
`token_use` e o scope da operação; o segredo de entrada (`PRELO_API_JWT_SECRET`) é
separado do segredo usado para chamar o llm-gateway. O healthcheck continua público. O modo
sem autenticação só é permitido explicitamente em loopback.

## Prelo — `POST /api/v1/tasks`

Cria uma task. `201 Created` com `{id, description, status, agentId}`.

```json
{"description":"o que é 2+2","context":[{"name":"nota","content":"responda em português"}],"agentId":"general"}
```

`agentId` é opcional (default `general`). `context` é opcional, limitado a 20 itens; a soma de `description` + conteúdo dos itens não pode ultrapassar 24000 caracteres.

## Prelo — `POST /api/v1/tasks/{taskId}/execute`

**`prelo-core`**: assíncrono (etapa 6.5/ADR-013). Retorna `202 Accepted` imediatamente:

```json
{"executionId":"...","taskId":"...","agentId":"general","status":"PENDING","result":null,"error":null}
```

A orquestração real (resolver agente, montar contexto, chamar o Gateway) acontece depois, em um worker consumindo a fila persistida. Use `GET /api/v1/tasks/{taskId}/executions/{executionId}` para acompanhar o resultado.

**`prelo` (Java)**: síncrono — a mesma chamada bloqueia até a execução terminar e retorna `200` com o resultado final já preenchido (`result`/`error`).

## Prelo — `GET /api/v1/tasks/{taskId}/executions/{executionId}` (etapa 6.5, só em `prelo-core`)

Consulta o estado de uma execution enfileirada:

```json
{"executionId":"...","taskId":"...","agentId":"general","status":"COMPLETED","result":"...","error":null,"requestId":"...","model":"...","provider":"...","startedAt":"...","completedAt":"..."}
```

`status` é um de `PENDING, RUNNING, COMPLETED, FAILED`. `404 execution_not_found` se o id não existir ou não pertencer à `taskId` informada.

## Prelo — `GET /api/v1/tasks/{taskId}`

`200` com `{id, description, status, agentId}`, `404 task_not_found` se não existir.

## Prelo — `POST /api/v1/chat`

Passthrough direto ao Gateway, sem passar por Task/Execution. Encaminha uma solicitação abstrata ao Gateway e persiste somente metadados de execução (auditoria em `llm_executions`).

```json
{"model":"mock-echo","messages":[{"role":"user","content":"Olá Prelo"}],"parameters":{"temperature":0.2,"maxTokens":128},"taskId":"task-1","agentId":"general"}
```

## Prelo — Ferramentas e aprovação (etapa 7/8, ADR-004, só `prelo-core`)

`GET /api/v1/tools` — `200` com `[{name, description, riskLevel}]`, o catálogo curado (`RiskLevel`: `LOW`/`MODERATE`/`HIGH`).

`POST /api/v1/executions/{executionId}/tools/{toolName}/invoke` body `{"args":"<json string>"}` (opcional, default `"{}"`) — sempre `200` com o `ToolCall` resultante (`{id, taskId, executionId, agentId, toolName, riskLevel, decision, outcome, result, error, createdAt, resolvedAt}`), mesmo quando `decision` é `DENY` ou `REQUIRE_APPROVAL` — essas são avaliações bem-sucedidas, não erros de request. `404 tool_not_found` se a ferramenta não existir no catálogo.

`GET /api/v1/approvals` — fila de `ApprovalRequest` com `status=PENDING` (expiração calculada na leitura, sem sweeper). `GET /api/v1/approvals/{id}` — um específico. `POST /api/v1/approvals/{id}/approve` / `.../deny` body `{"decidedBy":"<nome>"}` — aprovar executa a ferramenta na hora; `409 invalid_approval_transition` se já decidido ou expirado.

## Erros

Corpo `{code, message}` para os erros mapeados: `invalid_task_transition` (409), `invalid_approval_transition` (409, só `prelo-core`), `concurrent_modification` (409), `unknown_agent` (400), `task_not_found` (404), `execution_not_found` (404, só `prelo-core`), `tool_not_found` (404, só `prelo-core`), `gateway_failure` (502), `validation_error` (400, só `prelo-core`).

| Método/rota | Scope |
|---|---|
| `POST /api/v1/tasks` | `tasks:create` |
| `GET /api/v1/tasks/**` | `tasks:read` |
| `POST /api/v1/tasks/{id}/execute` | `tasks:execute` |
| `POST /api/v1/chat` | `chat:use` |
| `GET /api/v1/tools` / `POST .../invoke` | `tools:read` / `tools:invoke` |
| `/api/v1/approvals/**` | `approvals:read` / `approvals:decide` |
| observabilidade | `observability:read` |

## LLM Gateway — `POST /api/v1/llm/chat`

Não possui porta publicada. Exige Bearer JWT válido com issuer, audience, expiração e scope `llm:invoke`. O contrato é provider-neutro: `modelProfile`, `messages`, `parameters` e `metadata`; a resposta contém `id`, `provider`, `model`, `content`, uso e duração. `mock-echo` é o único modelo habilitado nesta etapa.

O Gateway nunca retorna ou registra credenciais de provider. Endpoints de saúde (`/actuator/health`) não exigem autenticação e também não são publicados para o Gateway.
