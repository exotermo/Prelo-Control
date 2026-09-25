# Current state — hermes-go em produção (cutover ADR-012/ADR-013)

**Cutover confirmado em 2026-09-23, autorizado explicitamente pelo dono do projeto.** `hermes-go`
é agora o serviço padrão (`docker compose up` sobe só ele). `hermes-app` (Java) foi **parqueado**,
não removido: código intacto em `hermes-app/`, mas excluído do `docker compose up` por padrão
(`profiles: ["legacy-java"]` em `compose.yaml` — suba com
`docker compose --profile legacy-java up hermes` se precisar comparar de novo).

O checklist de paridade formal (abaixo) não foi executado lado a lado ponto a ponto antes do
cutover — a decisão foi tomada com base na cobertura de teste extensa do `hermes-go` (G0–G12,
etapas 7/8) e na disponibilidade de `hermes-app/` intacto como fallback caso algo apareça em
produção. Isso é uma decisão explícita do dono do projeto, registrada aqui para rastreabilidade,
não uma alegação de que a paridade foi formalmente comprovada.

Implementado nesta etapa:

- `hermes-app`: Spring Boot Java 21 — **parqueado**, mantido só como referência de código, ver acima.
- `llm-gateway`: Spring Boot Java 21, OAuth2 Resource Server, validação JWT, scope `llm:invoke`, roteador e provider mock/Anthropic. Não muda com a reescrita Go (ADR-012). Segue em uso por `hermes-go`.
- PostgreSQL isolado, Flyway, logs correlacionados e auditoria sanitizada.
- Docker Compose com redes `hermes_internal` (interna) e `hermes_egress` (somente Gateway), sem Docker socket, host networking, modo privilegiado ou porta de banco.
- Hermes exposto somente em loopback para desenvolvimento.
- Core Domain inicial: `Task`, `Context`, `Directive`, `Agent` (somente `GeneralAgent`) e `Execution`, com estados explícitos.

## hermes-app-go (ADR-012, ADR-013) — serviço de produção

Reescrita completa de `hermes-app` em Go, `hermes-app-go/`, em `compose.yaml` (`hermes-go`,
`127.0.0.1:8082` — **nota**: a porta canônica 8080 continua ocupada por outro projeto não
relacionado neste host (`motoca-nginx`); `hermes-go` segue publicado em 8082 até isso ser
resolvido separadamente, decisão do operador da máquina, não deste projeto), contra o mesmo
Postgres/schema e o mesmo `llm-gateway`.

Slices completos (G0–G11), todos com teste automatizado verde e verificação ao vivo contra o stack real:

| Etapa | Entregável | Status |
|---|---|---|
| G0 | Esqueleto Go, health check, Dockerfile, entrada no compose | ✅ |
| G1 | Domínio (Task/Execution/AgentDefinition/ContextSnapshot), transições guardadas | ✅ 19 testes unitários |
| G2 | Migração `execution_jobs` (goose, V7) coexistindo com Flyway V1–V6 | ✅ verificado ao vivo |
| G3 | Repos Task/Execution com claim otimista versionado | ✅ inclui teste de concorrência (exatamente 1 vencedor) |
| G4 | Cliente Gateway + JWT HS256, paridade byte-a-byte com o Java | ✅ 4 testes (sucesso, 4xx, timeout, falha de transporte) |
| G5 | ContextResolver + repos de contexto | ✅ paridade de ordem/limites com o Java |
| G6 | API: `POST/GET tasks`, `POST hermes/chat` + auditoria `hermes_llm_executions` | ✅ verificado ao vivo, incl. chamada real ao Gateway |
| G7 | `/execute` síncrono temporário (paridade com Java) | ✅ verificado ao vivo, incl. 409/404 |
| G8 | Tabela/repo `execution_jobs`, claim atômico | ✅ teste de concorrência no claim do job |
| G9 | Redis Streams + worker pool, `/execute` vira `202 Accepted` real | ✅ verificado ao vivo (202 → processamento assíncrono → COMPLETED) |
| G10 | `GET /tasks/{id}/executions/{id}` (polling) | ✅ verificado ao vivo (PENDING→RUNNING→COMPLETED) |
| G11 | Sweeper: recuperação de jobs órfãos, retry/backoff, DEAD | ✅ teste de crash-recovery (exatamente 1 chamada real ao Gateway) |
| G12 | Atualização de docs + cutover | ✅ cutover confirmado 2026-09-23 (ver topo do arquivo) |
| 7 | ToolRegistry + PermissionPolicy (ADR-004) — catálogo de ferramentas curado, decisão ALLOW/DENY/REQUIRE_APPROVAL auditada em `tool_calls` | ✅ 5 testes (domínio + use case) + verificação ao vivo (LOW executa, sem capability nega, tool inexistente 404) |
| 8 | Approval workflow — `ApprovalRequest` vinculado a ação/escopo/expiração (15min), fila `GET /api/v1/approvals`, aprovar só roda depois do "sim" humano | ✅ verificação ao vivo (aprovar executa a ferramenta, aprovar 2x dá 409, negar nunca executa) |

### Checklist de paridade `hermes` (Java) vs `hermes-go` — não executado formalmente ponto a ponto

- [ ] Criação de task: mesmos códigos de erro (`unknown_agent`, `validation_error`/400 nativo do Spring) e mesmo corpo de sucesso.
- [ ] Execução: Java síncrono (`200` com resultado) vs Go assíncrono (`202` + poll) — comportamento HTTP diferente por design (ADR-013), resultado final equivalente.
- [ ] Erros 409 (`invalid_task_transition`, `concurrent_modification`) e 404 (`task_not_found`) em ambos.
- [ ] Snapshots de contexto: mesma ordem (`description` primeiro, depois itens manuais) e mesmos limites (20 itens, 24000 chars agregados).
- [ ] `POST /hermes/chat`: mesmo passthrough, mesma auditoria em `hermes_llm_executions`.

O cutover aconteceu sem fechar esta lista formalmente — decisão explícita do dono do projeto em
2026-09-23, com `hermes-app/` mantido intacto e reativável via
`docker compose --profile legacy-java up hermes` caso alguma divergência apareça depois.
Etapas 7 e 8 (ToolRegistry/PermissionPolicy/Approval, acima) só existem em `hermes-go` — não
foram portadas pro Java parqueado, e não serão.

## Bootstrap de identidade

O compose exige `.env` fora do Git. Nesta fundação, Hermes emite token de serviço HS256 de vida curta para o Gateway usando um segredo compartilhado externo (`HERMES_GATEWAY_JWT_SECRET`, reusado por `hermes` e `hermes-go`). É uma ponte de desenvolvimento para comprovar o Resource Server, não o IdP definitivo. Produção deve trocar o `JwtDecoder`/verificação por issuer/JWK de um IdP OIDC, removendo o segredo compartilhado.

O endpoint do `hermes-go` agora possui uma segunda fronteira de autenticação: JWTs de entrada do
messaging-core são validados com `HERMES_GO_API_JWT_SECRET`, issuer, audience, expiração, tenant,
token_use e scopes. Esse segredo não deve ser confundido nem reutilizado como credencial do
llm-gateway. O bridge envia o token de integração cacheado ao chamar o Hermes. Callbacks do
bridge usam contract v2 com timestamp e `X-Webhook-Id` persistido para impedir replay.

## Como executar

```bash
cp .env.example .env
# preencha valores aleatórios de ao menos 32 bytes
docker compose up -d --build              # sobe só o hermes-go (produção) + dependências
curl http://127.0.0.1:8082/actuator/health   # hermes-go
curl -X POST http://127.0.0.1:8082/api/v1/tasks -H 'content-type: application/json' -d '{"description":"olá"}'
curl http://127.0.0.1:8082/api/v1/tools      # ToolRegistry (etapa 7)
curl http://127.0.0.1:8082/api/v1/approvals  # fila de aprovações pendentes (etapa 8)
docker compose down
docker compose up -d

# hermes-app (Java) parqueado — só sobe se pedido explicitamente:
docker compose --profile legacy-java up -d hermes
curl http://127.0.0.1:8080/actuator/health
```

Para acesso remoto ao `maquiavel`, use túnel SSH; não altere os binds loopback para expor os serviços.
