# AGENTS.md

## Limites atuais

Esta etapa não implementa agentes autônomos. `hermes-app` apenas oferece a porta `LanguageModelGateway`, que será consumida pelo futuro orquestrador e pelos agentes definidos no domínio.

## Regras para agentes futuros

- Não chamar providers de LLM diretamente; usar o contrato do Gateway.
- Não receber, registrar ou pedir chaves de provider.
- Propagar `taskId`, `agentId` e `requestId` como metadados não sensíveis.
- Ferramentas e ações de risco passam pelo futuro `PermissionPolicy`, não pelo Gateway.
- Um agente não amplia seus próprios scopes nem decide sua autorização.
