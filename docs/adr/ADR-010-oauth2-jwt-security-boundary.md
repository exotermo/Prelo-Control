# ADR-010 — OAuth2 Resource Server e JWT

**Status:** Aceito com bootstrap de desenvolvimento

## Contexto

O Gateway precisa autenticar e autorizar clientes sem inventar protocolo proprietário.

## Decisão

Gateway é OAuth2 Resource Server e requer JWT com issuer, audience, expiração, `sub`, `client_id` e scope `llm:invoke`. Bootstrap local usa HS256 externo; produção deverá usar OIDC/JWK assimétrico de IdP.

## Alternativas

API keys do Prelo e autenticação própria foram descartadas.

## Consequências

Scopes permitem separar execução de administração futura. O segredo HS256 é temporário e exige proteção equivalente a credencial de serviço.

## Implicações de segurança

Tokens curtos, sem dados sensíveis, e validação de issuer/audience. Segredos não entram em Git ou logs.

## Evolução futura

Trocar decoder/configuração para Keycloak, Authentik ou IdP OIDC sem mudar o contrato LLM.
