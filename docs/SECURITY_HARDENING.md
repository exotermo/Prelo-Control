# Prelo — hardening implementado

Este documento registra o estado executável da rodada posterior à arquitetura técnica.

## API do prelo-core

As rotas `/api/v1/**` exigem JWT Bearer por padrão. O token precisa conter assinatura HS256
(com segredo separado do segredo usado para chamar o llm-gateway), `iss`, `aud`, `exp`,
`tenant_id`, `token_use` (`integration` ou `technical`) e pelo menos um scope. O algoritmo é
allowlist explícita; issuer, audience, validade temporal e scopes são verificados antes de o
handler ser chamado.

O modo sem autenticação só é permitido com `PRELO_ALLOW_INSECURE_LOCAL_ONLY=true` e
`PRELO_BIND_HOST=127.0.0.1` (ou loopback equivalente). O processo falha no startup se esse
modo for solicitado em endereço não-loopback.

Scopes atualmente aplicados:

- `tasks:create`, `tasks:read`, `tasks:execute`
- `chat:use`
- `tools:read`, `tools:invoke`
- `approvals:read`, `approvals:decide`
- `observability:read`

O middleware não substitui `PermissionPolicy`, allowlist, aprovação ou isolamento de tenant.

## Callbacks

O bridge consome contract v2 do messaging-core:

```text
X-Webhook-Timestamp: <epoch seconds>
X-Webhook-Id: <stable id across retries>
X-Signature: sha256=HMAC-SHA256(secret, timestamp + "." + rawBody)
```

O timestamp tem tolerância padrão de cinco minutos. O ID é reservado em `webhook_replays`
antes de JSON parsing ou dispatch, com chave primária para impedir replay concorrente e replay
após restart. O contrato body-only v1 está disponível somente por flag explícita de migração.

## Ferramentas

Chamadas de ferramenta continuam sujeitas à `PermissionPolicy` e agora também possuem limites
de timeout, argumentos, resposta e concorrência. Os limites são configuráveis por ambiente.
Não há promessa de sandbox de CPU/memória/filesystem para ferramentas in-process. Ferramentas
que executem processos ou acessem arquivos devem permanecer bloqueadas até serem movidas para
worker/container isolado com cgroups, filesystem allowlist e rede negada por padrão.

## Fora desta rodada

Ainda não foram implementados os registros comerciais `Opportunity`, `Evidence`, `Plan`,
`Business Approval` e `Artifact`, nem pagamento/planos ou `tool_use` nativo do Anthropic.
