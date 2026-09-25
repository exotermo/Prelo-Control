# ADR-012 — Reescrita de hermes-app em Go

**Status:** Aceito

## Contexto

`hermes-app` (Java/Spring) implementava as etapas 0–6 do núcleo (domínio, persistência, orquestração síncrona, contexto manual, integração com `llm-gateway`). O próximo passo, etapa 6.5, exige execução assíncrona durável com fila persistida, worker, retry e recuperação após restart. O usuário decidiu reescrever `hermes-app` inteiro em Go — não só a etapa 6.5 — antes de construir essa fila, mantendo `llm-gateway` em Java/Spring sem nenhuma alteração.

## Decisão

- `hermes-app` é reescrito do zero em Go (`hermes-app-go/`), reproduzindo fielmente o contrato HTTP com `llm-gateway`, o modelo de domínio (`Task`, `Execution`, `AgentDefinition`, `ContextSnapshot`), o schema Postgres existente (`hermes_app`, migrations V1–V6 do Flyway) e a API pública documentada em `API.md`.
- `llm-gateway` permanece Java/Spring, inalterado.
- Estratégia de corte: **novo módulo em paralelo**. `hermes-app-go/` foi desenvolvido e testado contra o mesmo Postgres e o mesmo `llm-gateway` que `hermes-app` (Java) já usa, sem tocar ou remover o módulo Java. Ambos coexistem em `compose.yaml` como serviços distintos (`hermes`, porta 8080; `hermes-go`, porta 8082).
- Migrations do Go (goose, a partir de V7) usam uma tabela de histórico própria (`hermes_app.hermes_go_schema_history`), separada da tabela do Flyway (`hermes_flyway_schema_history`), para nunca colidir.

## Alternativas

- Reescrever só a etapa 6.5 em Go, mantendo o resto em Java: descartado pelo usuário — preferiu uma stack única para o núcleo.
- Substituir `hermes-app/` diretamente (sem paralelo): descartado por deixar o sistema quebrado durante toda a reescrita e por perder o Java como referência de comparação.

## Consequências

- Dois serviços escrevem no mesmo schema Postgres durante a transição — aceitável porque cada um usa sua própria tabela de histórico de migração e os dois seguem o mesmo modelo de dados.
- Critério de cutover: paridade funcional validada lado a lado (ver checklist em `CURRENT_STATE.md`) antes de qualquer alteração em `compose.yaml` que troque `hermes` por `hermes-go` como serviço publicado, e antes de remover `hermes-app/` (Java). Nenhuma das duas ações é automática — exigem confirmação explícita do usuário.
- O futuro serviço de mensageria genérico (WhatsApp/Telegram/etc., escopo diferente e posterior) será Java 21 + OAuth2, não Go — esta decisão não estabelece Go como padrão para novos serviços do projeto, é específica a `hermes-app`.

## Implicações de segurança

Nenhuma mudança de superfície: `hermes-go` usa os mesmos segredos (`HERMES_GATEWAY_JWT_SECRET`, `POSTGRES_PASSWORD`) via as mesmas convenções de `.env`, e respeita o isolamento de rede do ADR-008 (nenhuma porta nova exposta ao host além do padrão já usado por `hermes`).

## Evolução futura

Etapa 6.5 (fila assíncrona) nasce diretamente em Go — ver ADR-013. Após validação e cutover, `hermes-app/` (Java) é removido como decisão separada.
