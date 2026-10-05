# Contrato v1 — Push no celular (G11)

> **Status: IMPLEMENTADO no servidor** (2026-10-04; migration 00028). Desligado até o dono montar a conta de
> serviço do Firebase (`PRELO_FCM_SERVICE_ACCOUNT_FILE` no `.env`). O app pode registrar o token desde já.

## O que é

Firebase Cloud Messaging (HTTP v1) avisando o celular de que **há algo esperando**. A notificação nunca traz
conteúdo (nem descrição da task, nem projeto, nem comando): só um texto fixo e ids opacos. Ao tocar, o app
abre a tela e busca os detalhes pela API normal, que aplica toda a autorização.

Push é melhor esforço. O WhatsApp do dono, o dashboard e o app aberto (G4) continuam sendo a fonte da verdade.

## Registro do token (app)

Só **sessão do app** (access token com `sid`). Sessão web → `403 mobile_session_required`.

- `PUT /api/v1/me/push-token` `{ "token": "<token de registro do FCM>" }`
  → `200 { "registered": true, "pushEnabled": true|false }`
  - `pushEnabled: false` = o servidor ainda não tem credencial do Firebase; o token fica guardado mesmo assim.
  - `400` token vazio, com espaço ou maior que 4096 caracteres; `401` sessão encerrada.
- `DELETE /api/v1/me/push-token` → `204` (o usuário desligou as notificações no app).
- Quando chamar o PUT: depois do login (`mobile/verify`), em `FirebaseMessagingService.onNewToken` e ao abrir o
  app se o token mudou. Um token por sessão (aparelho); se o mesmo token for registrado em outra sessão, a
  anterior perde o token.
- Logout, revogação, troca de papel ou expiração da sessão → para de receber push na hora (nada a fazer no app).
- Token que o FCM diz não existir mais (`UNREGISTERED`) é apagado pelo servidor.

## Quem recebe

Mesma regra do fluxo de eventos: ADMIN recebe tudo; OPERATOR só de projetos de que é membro; itens sem
projeto vão para todos. Só sessões de app ativas e com token.

## Mensagens

| Quando | `notification.body` | `data` |
|---|---|---|
| Nova aprovação pendente (ferramenta de agente ou pedido externo) | "Há uma aprovação esperando você." | `kind=approval`, `id=<approvalId>`, `projectId?`, `actionRequestId?` |
| Deploy terminou (`SUCCEEDED`/`FAILED`/`ROLLED_BACK`/`CANCELLED`) | "Um deploy terminou com sucesso." / "…falhou." / "…foi revertido." / "…foi cancelado." | `kind=action`, `id=<actionRequestId>`, `status`, `projectId?` |

- `notification.title` = "Prelo Control".
- Android: `priority HIGH`, **canal `prelo_alerts`** (o app precisa criá-lo), `tag`/`collapse_key` `approval`
  (aprovações novas substituem a anterior na bandeja) ou `action-<id>`.
- Ao tocar: `kind=approval` → tela da aprovação `id` (usar `projectId` como `X-Project-Id` se vier);
  `kind=action` → deploys do projeto. Se a API responder 403/404, mostrar "sem acesso" e não insistir.
- Nenhum outro campo deve ser esperado em `data`; o app nunca deve depender de conteúdo vindo do push.

## Servidor (operação)

1. Console do Firebase → criar projeto → adicionar app Android com o `applicationId`
   (`com.workcontrol.app` e `com.workcontrol.app.debug`).
2. Configurações do projeto → Contas de serviço → **Gerar nova chave privada** (JSON).
3. Guardar fora do repositório (ex.: `~/.config/prelo/fcm-service-account.json`, pasta `chmod 700`, arquivo
   `chmod 644` — o container lê como usuário `prelo`) e pôr o caminho absoluto em
   `PRELO_FCM_SERVICE_ACCOUNT_FILE` no `.env`.
4. `docker compose up -d --no-deps prelo-core` → log `push: FCM on (Firebase project …)`.

O `google-services.json` do app **não é segredo do servidor** (identifica o app no Firebase), mas mesmo assim
fica fora do repositório do Work Control (ver prompt do app).
