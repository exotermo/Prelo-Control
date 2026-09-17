# Current state — foundation slice

Implementado nesta etapa:

- `hermes-app`: Spring Boot Java 21, health endpoint, cliente do Gateway e persistência de metadados de execução;
- `llm-gateway`: Spring Boot Java 21, OAuth2 Resource Server, validação JWT, scope `llm:invoke`, roteador e provider mock;
- PostgreSQL isolado, Flyway, logs correlacionados e auditoria sanitizada;
- Docker Compose com redes `hermes_internal` (interna) e `hermes_egress` (somente Gateway), sem Docker socket, host networking, modo privilegiado ou porta de banco;
- Hermes exposto somente em loopback para desenvolvimento.
- Core Domain inicial: `Task`, `Context`, `Directive`, `Agent` (somente `GeneralAgent`) e `Execution`, com estados explícitos.
- Fluxo de aplicação: criar task → orquestrar GeneralAgent → `LlmClient` → Gateway → resultado/execution; `GatewayLlmClient` é o único adapter novo que liga o core ao contrato público do Gateway.
- API mínima: `POST /api/v1/tasks`, `POST /api/v1/tasks/{taskId}/execute` e `GET /api/v1/tasks/{taskId}`.
- Persistência: migration V2 cria `tasks` e `task_executions` no schema `hermes_app`; nenhum schema ou tabela do Gateway foi alterado.

## Bootstrap de identidade

O compose exige `.env` fora do Git. Nesta fundação, Hermes emite token de serviço HS256 de vida curta para o Gateway usando um segredo compartilhado externo. É uma ponte de desenvolvimento para comprovar o Resource Server, não o IdP definitivo. Produção deve trocar o `JwtDecoder` por issuer/JWK de um IdP OIDC e Hermes por client credentials/OAuth2, removendo o segredo compartilhado.

## Como executar

```bash
cp .env.example .env
# preencha valores aleatórios de ao menos 32 bytes
docker compose up -d --build
curl http://127.0.0.1:8080/actuator/health
curl -X POST http://127.0.0.1:8080/api/v1/hermes/chat -H 'content-type: application/json' \
  -d '{"model":"mock-echo","messages":[{"role":"user","content":"olá"}]}'
docker compose down
docker compose up -d
```

O smoke test esperado retorna conteúdo `[mock] olá` e três serviços saudáveis. Para acesso remoto ao `maquiavel`, use túnel SSH; não altere o bind loopback para expor o serviço.
