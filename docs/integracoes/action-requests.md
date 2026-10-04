# Contrato v1 — Pedidos de ação externa (`action-requests`)

> **Status: CONGELADO (Fase 0), ainda não implementado no Prelo** (PR-3). Lacunas G5–G8 de
> `docs/CONTRATOS.md`. Quem integra (BastionDeploy) implementa contra este documento e usa um stub até o
> Prelo publicar. Mudança aqui só por proposta em `docs/integracoes/PROPOSTAS.md`.

## Por que existe

O Prelo é a única autoridade de autorização para ações de risco. Hoje a aprovação só nasce de uma
ferramenta chamada por um agente. Um sistema externo (o BastionDeploy) precisa pedir: "posso implantar o
commit X no destino Y?" — e receber uma decisão **amarrada exatamente a esse conteúdo**.

## Autenticação e escopos

- Chave de API do projeto (`prl_live_…`, presa a **um** projeto) criada em Projetos → Integrações.
- Escopos novos: `actions:request` (criar/ler pedidos) e `actions:report` (reportar o resultado).
  Nunca `approvals:decide` — sistema externo não decide.
- `projectId` do corpo deve ser o projeto da chave (senão 403).

## Recurso

```
ActionRequest {
  id: UUID                      // gerado pelo Prelo
  workspaceId: string           // instância do Prelo (v1: um valor fixo por instalação)
  projectId: UUID
  kind: "deploy"                // v1; outros tipos entram por proposta
  payload: object               // conteúdo que está sendo autorizado (ver "Payload de deploy")
  payloadHash: string           // "sha256:<64 hex>" do payload canônico — calculado pelos DOIS lados
  risk: "LOW" | "MODERATE" | "HIGH"   // deploy é sempre HIGH (o Prelo força)
  impact: string                // frase em pt-BR mostrada ao dono (≤ 300)
  requestedBy: string           // "github:<owner>/<repo>@<workflow_ref>" | "prelo:agent:<agentId>" | "user:<id>"
  idempotencyKey: string        // ≤ 200; mesmo valor = mesmo pedido
  status: "PENDING" | "APPROVED" | "DENIED" | "EXPIRED"
  approvalCode: string          // código curto do WhatsApp (4 letras)
  expiresAt: RFC3339            // decisão só vale até aqui (30 min)
  decidedAt?: RFC3339, decidedBy?: string
  result?: ActionResult         // ver /result
  createdAt: RFC3339
}
```

## Payload de deploy (kind = "deploy")

```json
{
  "repository": "owner/name",
  "commitSha": "40 caracteres hexadecimais minúsculos",
  "environment": "production",
  "target": "app.cliente.com.br",
  "app": "nome-do-app",
  "deployRequestId": "id no BastionDeploy"
}
```

Todos obrigatórios. `commitSha` nunca é nome de branch. Outro SHA, ambiente ou destino = **outro pedido**.

## Hash canônico (os dois lados precisam bater)

`payloadHash = "sha256:" + hex(SHA-256(canonical(payload)))`, onde `canonical` é JSON em UTF-8, **chaves
ordenadas lexicograficamente em todos os níveis**, sem espaços (`,` e `:`), strings com escape padrão JSON,
sem `\u` desnecessário para não-ASCII. O Prelo recalcula e recusa (422) se não bater.

Exemplo de teste de contrato: payload acima com
`deployRequestId="d1", app="loja", environment="production", target="loja.example.com",
repository="exotermo/loja", commitSha="0123456789abcdef0123456789abcdef01234567"` →
canônico
`{"app":"loja","commitSha":"0123456789abcdef0123456789abcdef01234567","deployRequestId":"d1","environment":"production","repository":"exotermo/loja","target":"loja.example.com"}`
e hash `sha256:10c22d67a404931356d344a6c56a7161e8a4b156269b1eae19331fcdc7773a6a` (vetor de teste: os dois
lados devem reproduzir exatamente esse valor).

## Endpoints

### `POST /api/v1/action-requests`
Corpo: `{kind, projectId, payload, payloadHash, impact, requestedBy, idempotencyKey}`.
- `201` + `ActionRequest` (novo, `PENDING`); o Prelo notifica o dono (WhatsApp + dashboard + Work Control).
- `200` + o **mesmo** `ActionRequest` se `idempotencyKey` já existe **com o mesmo payloadHash**.
- `409 idempotency_conflict` se a chave existe com outro payloadHash.
- `400` validação, `403` projeto/escopo, `422 payload_hash_mismatch`.

### `GET /api/v1/action-requests/{id}`
Estado atual (fonte de verdade da decisão). O executor **sempre** consulta antes de executar, mesmo tendo
recebido o webhook.

### `POST /api/v1/action-requests/{id}/result` (escopo `actions:report`)
```json
{ "status": "RUNNING" | "SUCCEEDED" | "FAILED" | "ROLLED_BACK" | "CANCELLED",
  "message": "≤ 500, sem segredos", "url": "https://…", "artifactDigest": "sha256:…",
  "reportedAt": "RFC3339", "sequence": 3 }
```
- Só aceito se o pedido está `APPROVED`; `409` caso contrário (executar sem aprovação é erro do executor).
- Idempotente por `(id, sequence)`; sequência menor que a última é ignorada (`200`, sem efeito).

### `GET /api/v1/projects/{projectId}/actions?kind=deploy&limit=50`
Leitura para web e Work Control (sessão de dashboard/mobile, `projects:read` + membro do projeto).

## Webhook `action.decided`

Pelo mecanismo de webhooks do projeto (assinatura `X-Prelo-Signature`). Corpo:
`{ "event": "action.decided", "actionRequestId", "status": "APPROVED|DENIED|EXPIRED", "payloadHash",
"decidedAt", "decidedBy" }`. É só um aviso: o executor confirma com `GET` antes de agir.

## Máquina de estados

```
PENDING ──SIM <código> / Aprovar──▶ APPROVED ──/result RUNNING…──▶ (SUCCEEDED | FAILED | ROLLED_BACK | CANCELLED)
   │──NÃO <código> / Negar──▶ DENIED      (terminal; nenhuma execução)
   └──expiresAt passou──▶ EXPIRED         (terminal; nenhuma execução)
```

Regras: decisão é de uso único; `APPROVED` não reabre; um `APPROVED` não autoriza nenhum outro
`payloadHash`; a execução precisa **começar** antes de `expiresAt + 10 min` (executor recusa depois e
pede de novo).

## Auditoria

Toda criação, decisão e resultado fica registrada no Prelo com quem, quando e `payloadHash`, visível na
linha do tempo do projeto/cliente.
