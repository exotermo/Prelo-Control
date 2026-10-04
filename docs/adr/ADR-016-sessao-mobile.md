# ADR-016 — Sessão do app móvel com a identidade do Prelo

**Status:** Aceito (2026-10-04) — implementação no PR-2 do Prelo

## Contexto

A sessão do dashboard web usa access token JWT de 15 min e refresh token em **cookie HttpOnly** (7 dias,
`dashboard_auth_tokens` purpose REFRESH). Um app nativo não tem um jar de cookies confiável e persistente, e
o Work Control usava um segundo sistema de identidade (Keycloak/OIDC) que o dono não quer manter.

## Decisão

- Mesmo login (e-mail + senha + TOTP) e mesmo `token_use=dashboard` do access token; o refresh do app vem
  **no corpo**, opaco, **ligado a um `deviceId`**, com **rotação a cada uso e detecção de reuso** (reuso revoga
  a família), validade deslizante de 30 dias e máximo de 90.
- O app guarda o refresh cifrado com chave do Android Keystore que exige autenticação do usuário
  (biometria/credencial do aparelho). Biometria desbloqueia; o servidor autentica.
- Sessões por dispositivo listáveis e revogáveis no web; desativar usuário ou mudar papel revoga.
- Aprovação de risco ALTO pelo app exige TOTP recente (step-up).

## Consequências

- Nenhum segredo no APK; nada de Keycloak; uma única tabela de usuários.
- Novo endpoint de `/me`, endpoints `mobile/*` e tela de sessões no dashboard (contrato em
  `docs/integracoes/sessao-mobile.md`).
