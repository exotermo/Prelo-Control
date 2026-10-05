# Contrato v1 — Eventos em tempo real (G4)

> **Status: IMPLEMENTADO** no PR-4 (2026-10-04; migration 00027). Para o dashboard web e o Work Control.

## O que é

Um fluxo **Server-Sent Events** que avisa *que* algo mudou. Não carrega conteúdo: ao receber um evento, a tela
busca de novo pela API normal (que aplica toda a autorização). Polling continua como reserva, bem mais espaçado
enquanto o fluxo está conectado.

## Endpoint

`GET /api/v1/events/stream` — sessão de pessoa (web ou app; escopo `projects:read`). Chave de integração → 403.

- Autenticação **só pelo cabeçalho** `Authorization: Bearer <accessToken>`. Nunca token na URL.
  - Navegador: `fetch` com leitura do `body` em streaming (o `EventSource` nativo não aceita cabeçalhos).
  - Android: OkHttp `EventSource` (okhttp-sse) com o cabeçalho, ou leitura de linhas do corpo.
- Até 5 conexões simultâneas por usuário (`429 too_many_streams` além disso).
- Comentário `: ping` a cada 25 s; reconectar com espera crescente se cair (o servidor envia `retry: 5000`).
- Sessão do app encerrada (logout, revogação, troca de papel) → o servidor manda `event: session_ended` e fecha.
- Quando o access token expirar (15 min), reconectar com o novo token.

## Formato

```
event: <kind>
data: {"kind":"task","id":"uuid","projectId":"uuid|null","status":"RUNNING","at":"…", ...}
```

| kind | quando | campos extras |
|---|---|---|
| `ready` | logo ao conectar | — |
| `task` | task criada ou mudou de status | `parentId` (subtask) |
| `execution` | turno de execução registrado/atualizado (`status` = LLM_CALL/TOOL_CALL) | `taskId` |
| `approval` | aprovação criada ou decidida/expirada | `actionRequestId` (se for pedido externo) |
| `action` | resultado de pedido externo mudou (`status` = RUNNING/SUCCEEDED/…) | — |
| `resync` | o servidor pode ter perdido eventos (reconexão, cliente lento) | — recarregue tudo o que está na tela |
| `session_ended` | sessão do app encerrada | — volte ao login |

## Visibilidade

ADMIN recebe tudo; OPERATOR recebe eventos dos projetos de que é membro; eventos sem projeto (`projectId: null`,
ex.: conversas do WhatsApp) vão para todos — mesma regra da tela de Tasks. A lista é reavaliada a cada ping.
