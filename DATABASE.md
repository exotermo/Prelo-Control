# Database

PostgreSQL é a única persistência da topologia inicial e não publica porta no host. Os serviços usam a rede `prelo_internal`; somente Prelo e Gateway possuem credenciais de banco em runtime.

| Tabela | Dono lógico | Conteúdo | Não armazena |
|---|---|---|---|
| `llm_executions` | Prelo | request/task/agent IDs, modelo, provider, duração e status do passthrough `/chat` | prompt, resposta, JWT ou segredo |
| `gateway_audit_events` | LLM Gateway | evento de segurança/uso e correlação | chaves de provider, JWT e conteúdo LLM |

`llm_executions` é legado do V1 mas segue ativo: é a tabela de auditoria do endpoint passthrough `POST /chat`, escrita tanto por `prelo` (Java) quanto por `prelo-core` (mesmo formato).

Cada serviço controla suas migrações em uma tabela de histórico distinta, preservando autonomia sem introduzir outro banco. O volume `postgres_data` conserva dados entre `down` e `up`; removê-lo é uma operação destrutiva e intencional.

## Migrações do núcleo (schema `prelo_app`)

`prelo` (Java) usa Flyway, histórico em `prelo_app.legacy_flyway_schema_history`, migrações V1–V6: `tasks`, `task_executions`, `task_manual_context_items`, `context_snapshots`, `context_snapshot_items` (ver ADR-002/003 para o modelo de domínio por trás).

`prelo-core` (Go, ADR-012) usa `goose`, histórico em `prelo_app.prelo_core_schema_history` — uma tabela própria, deliberadamente separada da do Flyway para nunca colidir, embora ambos os serviços leiam/escrevam o mesmo schema `prelo_app`. As migrações do Go continuam a numeração a partir de V7.

### `execution_jobs` (V7, goose — etapa 6.5/ADR-013)

Fila persistida de execução, 1:1 com `task_executions` via `execution_id` único. Metadado de agendamento — `task_executions` continua sendo o registro de auditoria/resultado, sem mudança de schema.

| Coluna | Tipo | Notas |
|---|---|---|
| `id` | UUID PK | mesmo valor de `execution_id` — também é o `requestId` enviado ao Gateway (chave de idempotência) |
| `task_id` | UUID FK→`tasks` | |
| `execution_id` | UUID FK→`task_executions`, único | |
| `status` | VARCHAR(32) | `PENDING, CLAIMED, RUNNING, DONE, FAILED, RETRY, DEAD` |
| `priority` | SMALLINT | default 0 |
| `attempt` / `max_attempts` | INT | default 0 / 5 |
| `available_at` | TIMESTAMPTZ | job não reivindicável antes deste instante (backoff) |
| `claimed_by` / `claimed_at` | VARCHAR(100) / TIMESTAMPTZ | id do worker e instante do claim |
| `lease_expires_at` | TIMESTAMPTZ | claim considerado órfão (worker morto) após este instante — chave da recuperação do sweeper |
| `last_error` | VARCHAR(1000) | |
| `job_version` | BIGINT | usado pelo claim atômico (`UPDATE ... WHERE status IN (...)`, não um lock otimista clássico — ver ADR-013) |

Índices: `execution_jobs_execution_id_uk` (único), `execution_jobs_claimable_idx` (parcial, `status IN ('PENDING','RETRY')`), `execution_jobs_lease_idx` (parcial, `status IN ('CLAIMED','RUNNING')`).

### `tool_calls` e `approval_requests` (V8, goose — etapas 7/8/ADR-004)

`tool_calls`: auditoria de toda invocação de ferramenta, permitida ou não — a linha é escrita antes de qualquer execução, então `decision`/`outcome` nulo/pendente também é um registro válido, nunca um buraco na trilha.

| Coluna | Tipo | Notas |
|---|---|---|
| `id` | UUID PK | |
| `task_id` / `execution_id` | UUID FK | |
| `agent_id` | VARCHAR(200) | |
| `tool_name` | VARCHAR(200) | |
| `args_json` | TEXT | |
| `risk_level` | VARCHAR(20) | `LOW, MODERATE, HIGH` |
| `decision` | VARCHAR(30) | `ALLOW, DENY, REQUIRE_APPROVAL` — veredito do `PermissionPolicy`, nunca revisto depois |
| `outcome` | VARCHAR(20), nullable | `EXECUTED, FAILED, DENIED, EXPIRED` — nulo até a chamada ser resolvida |
| `result` / `error` | TEXT / VARCHAR(1000), nullable | |
| `created_at` / `resolved_at` | TIMESTAMPTZ | |
| `call_version` | BIGINT | lock otimista clássico (`WHERE call_version = $N`) |

Índice: `tool_calls_execution_idx`.

`approval_requests`: no máximo uma linha por `tool_call_id` (`REQUIRE_APPROVAL` só acontece uma vez por chamada — uma chamada nova, mesmo com args idênticos, sempre gera seu próprio par `tool_call`+`approval_request`).

| Coluna | Tipo | Notas |
|---|---|---|
| `id` | UUID PK | |
| `tool_call_id` | UUID FK, único | |
| `scope` | TEXT | descrição legível do que rodaria, mostrada ao aprovador verbatim |
| `status` | VARCHAR(20) | `PENDING, APPROVED, DENIED, EXPIRED` |
| `requested_at` / `expires_at` | TIMESTAMPTZ | expiração calculada na leitura (`EffectiveStatus`), sem sweeper — default 15min |
| `decided_at` / `decided_by` | TIMESTAMPTZ / VARCHAR(200), nullable | |
| `approval_version` | BIGINT | lock otimista clássico |

Índices: `approval_requests_tool_call_uk` (único), `approval_requests_pending_idx` (parcial, `status = 'PENDING'`).

### `tasks.tenant_id` (V11, goose)

Tasks criadas pela API autenticada carregam o UUID do `tenant_id` extraído do JWT verificado.
Linhas legadas permanecem `NULL` até uma migração de ownership explícita e não são retornadas
por consultas tenant-scoped da API. O índice parcial `tasks_tenant_id_created_at_idx` apoia a
listagem por tenant sem alterar o histórico legado.
