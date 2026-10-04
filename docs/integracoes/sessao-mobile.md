# Contrato v1 — Sessão do app (Work Control) e identidade

> **Status: implementado** — G1 e G3 no PR-1, G2 e G9 no PR-2 (2026-10-04). Detalhes da implementação:
> refresh tem 256 bits aleatórios (só o hash é guardado), access token do app carrega `sid` e é recusado
> na hora quando a sessão é encerrada, e a resposta de login/refresh é
> `{accessToken, expiresIn, refreshToken, refreshExpiresAt, sessionId}`. Lacunas G1, G2, G3 e G9
> de `docs/CONTRATOS.md`. Identidade única = a do Prelo (e-mail + senha + TOTP). Nada de Keycloak.

## Princípios

- O servidor decide tudo: identidade, papel, projeto, escopo. O app só exibe e pede.
- Nenhum segredo no APK, no `build.gradle.kts`, em SharedPreferences em claro ou em cache comum.
- Access token só em memória (15 min). Refresh token opaco, **um por dispositivo**, guardado cifrado com chave
  do **Android Keystore** que exige autenticação do usuário (biometria ou credencial do aparelho).
- Biometria **só desbloqueia** o refresh local; não é identidade. Perder o aparelho = revogar a sessão no web.

## G3 — `GET /api/v1/me`

Qualquer sessão (web ou mobile).
```json
{ "userId": "uuid", "email": "…", "role": "ADMIN|OPERATOR", "scopes": ["…"],
  "workspaceId": "…", "workspaceName": "…",
  "projects": [{ "id": "uuid", "name": "…", "clientId": "uuid|null" }],
  "session": { "kind": "web|mobile", "deviceId": "uuid|null", "deviceName": "…|null" } }
```

## G1 — Workspace

v1: a instância do Prelo é o workspace. `workspaceId` é um UUID estável gerado uma vez pela migration 00024
(tabela `workspace`, linha única) e aparece em `/me` e em todo `action-request`. Multi-workspace é evolução
compatível (campo já existe).

## G2 — Fluxo mobile

1. `POST /api/v1/dashboard-auth/login` `{email, password}` → `{challenge, nextStep}` (igual ao web).
   `TOTP_SETUP_REQUIRED` no app → orientar a configurar pelo navegador (o app não faz setup de TOTP v1).
2. `POST /api/v1/dashboard-auth/mobile/verify`
   `{challenge, code, deviceId (UUID gerado na instalação), deviceName ("Moto g54"), platform: "android"}`
   → `{accessToken, expiresIn, refreshToken, refreshExpiresAt}`. Código de recuperação também aceito.
3. `POST /api/v1/dashboard-auth/mobile/refresh` `{refreshToken, deviceId}` → novo par. **Rotação**: o refresh
   usado morre; reapresentar um refresh já usado revoga **toda** a família daquele dispositivo (roubo).
   Validade do refresh: 30 dias deslizantes, máximo absoluto 90 dias → novo login com TOTP.
4. `POST /api/v1/dashboard-auth/mobile/logout` `{refreshToken, deviceId}` → revoga a família.
5. Sessões: `GET /api/v1/me/sessions` (lista dispositivos: nome, plataforma, último uso, IP aproximado),
   `DELETE /api/v1/me/sessions/{sessionId}` (revoga). ADMIN pode revogar sessões de outros usuários.
   Usuário desativado ou papel alterado → sessões revogadas.

Erros: `401 invalid_refresh` (app volta ao login), `401 refresh_reused` (idem + aviso); verify com código
errado segue as mesmas regras do login web (tentativas por desafio, bloqueio por conta).
Sessões também caem quando um admin troca o papel do usuário ou quando a senha é redefinida.
Admin: `GET/DELETE /api/v1/users/{userId}/sessions` (escopo `users:manage`).

## G9 — Confirmação reforçada para risco ALTO

`POST /api/v1/approvals/{id}/approve` de uma aprovação HIGH vinda de sessão mobile exige
`{ "totpCode": "123456" }` (ou TOTP validado nos últimos 5 min na mesma sessão — o próprio login conta).
Sem isso: `403 step_up_required`. Código de recuperação não vale aqui. Negar nunca exige.

## Autorização no servidor (sem mudanças de regra)

`X-Project-Id` + membership (OPERATOR) / ADMIN em tudo; o app recebe 403 e não mostra o recurso. O app
nunca decide permissão olhando o próprio token.

## O que o app guarda

| Item | Onde | Proteção |
|---|---|---|
| accessToken | memória do processo | expira em 15 min |
| refreshToken | arquivo cifrado (AES-GCM, chave Keystore `setUserAuthenticationRequired`) | biometria/credencial do aparelho; apagado no logout |
| deviceId | DataStore | não é segredo |
| API base URL | `BuildConfig` por variante | não é segredo |
