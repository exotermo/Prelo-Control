# Propostas de mudança de contrato

Work Control e BastionDeploy dependem de contratos estáveis do Prelo. Quem descobrir que falta algo **não
altera o Prelo**: registra aqui, e a mudança é analisada no fluxo principal do Prelo (Claude + dono).

## Como propor

Copie o bloco abaixo, preencha e abra um PR só com este arquivo (ou anexe ao PR do seu repositório).

```
### P-<número> — <título curto>
- Origem: work-control | bastiondeploy
- Status: proposta | aceita | recusada | implementada (PR do Prelo)
- Problema: o que não dá para fazer hoje e por quê
- Contrato proposto: endpoint/evento/schema (JSON de exemplo, erros, escopo)
- Alternativa sem mudar o Prelo: (se existir)
- Impacto de segurança: quem passa a poder fazer o quê
- Prazo/necessidade: bloqueia qual fatia
```

## Propostas do Codex para o Prelo

_(nenhuma ainda)_

## Pedidos do Prelo para os outros produtos

### P-1 — Bastion aceitar pedido de deploy vindo de um agente do Prelo (para o G10 `request_deploy`)
- Origem: prelo → bastiondeploy
- Status: proposta (2026-10-04)
- Problema: hoje `POST /api/v1/deploy/intents` do Bastion só aceita a GitHub Action (HMAC com
  `GITHUB_WEBHOOK_SECRET`) e um único app (`BASTION_ALLOWED_REPOSITORY`/`BASTION_APP`…). Um agente do Prelo não
  tem como pedir "implante o commit X do app Y em staging".
- Contrato proposto (lado Bastion):
  - `POST /api/v1/deploy/intents/prelo` com `X-Prelo-Signature: sha256=HMAC(segredo, timestamp + "." + corpo)` e
    `X-Prelo-Timestamp` (mesmo esquema dos webhooks do Prelo; recusar timestamp com mais de 5 min).
  - Corpo: `{repository, commitSha (40 hex), environment, app, projectId, requestedBy: "prelo:agent:<agentId>",
    taskId, requestId}` — `target` é resolvido pelo Bastion a partir de app+ambiente (o agente não escolhe destino).
  - O Bastion valida repo/app/ambiente contra a própria configuração e cria o `action-request` no Prelo como já
    faz (a aprovação é a mesma: dono decide; nada executa sem `APPROVED`).
  - Resposta `202 {deployId, actionRequestId, approvalCode}`; idempotente por (repo, SHA, ambiente).
  - Erros: `400` validação, `401` assinatura, `404` app/ambiente desconhecido, `409` SHA não pertence ao repositório.
- Do lado do Prelo (depois): ferramenta `request_deploy` (risco LOW — só cria o pedido; a autorização é a do
  action-request), com URL do Bastion e segredo guardados cifrados por projeto (Integrações).
- Alternativa sem mudar o Bastion: nenhuma segura (reusar o segredo da GitHub Action no Prelo misturaria as identidades).
- Impacto de segurança: o Prelo passa a poder *pedir* deploy; executar continua dependendo da aprovação do dono.
