# API

## Hermes — `POST /api/v1/hermes/chat`

Único endpoint publicado localmente em `127.0.0.1:8080`. Encaminha uma solicitação abstrata ao Gateway e persiste somente metadados de execução.

```json
{"model":"mock-echo","messages":[{"role":"user","content":"Olá Hermes"}],"parameters":{"temperature":0.2,"maxTokens":128},"taskId":"task-1","agentId":"general"}
```

## LLM Gateway — `POST /api/v1/llm/chat`

Não possui porta publicada. Exige Bearer JWT válido com issuer, audience, expiração e scope `llm:invoke`. O contrato é provider-neutral: `model`, `messages`, `parameters` e `metadata`; a resposta contém `id`, `provider`, `model`, `content`, uso e duração. `mock-echo` é o único modelo habilitado nesta etapa.

O Gateway nunca retorna ou registra credenciais de provider. Endpoints de saúde (`/actuator/health`) não exigem autenticação e também não são publicados para o Gateway.
