# Diagnóstico — BastionDeploy e Work Control frente ao Prelo Control

> Levantado em 2026-10-04, só leitura, a partir dos remotos atuais
> (`~/projects/BastionDeploy`, `~/projects/work-control-mvp`). Contratos do Prelo: `docs/CONTRATOS.md`.

## 1. BastionDeploy (`exotermo/BastionDeploy`)

**Estado real:** 16 commits, último em 06/04/2026. ~1.750 linhas de Go + dashboard React.
- `api/` (Gin): webhook do GitHub com HMAC SHA-256 em tempo constante (bom), chave de API única
  (`API_KEY`), Postgres (tabela de deploys), fila no Redis (`LPUSH bastiondeploy:jobs`), estatísticas.
- `agent/` (Go): `BRPOP` da fila → `git clone --depth=1 --branch <branch>` → `docker build` → `docker run
  -d --restart unless-stopped -p 127.0.0.1:porta` → Nginx ou script de cloudflared (precisa de `sudo`) →
  aviso no Discord.
- `cli/` (exoctl, wizard de setup), `dashboard/` (React + Nginx).
- **Sem testes** (`tests/` vazio). README fala em "ExoDeploy/exoctl"; `info.md` descreve um desenho antigo
  em Python — documentação desatualizada.

**Lacunas frente à visão (deploy aprovado de um commit exato, isolado):**
1. **Implanta a ponta da branch, não o SHA**: o clone usa `--branch`; o commit aprovado pode não ser o
   implantado.
2. **Sem aprovação humana**: webhook válido = job na fila na hora.
3. **Sem idempotência**: reentrega do GitHub ou dois pushes criam dois deploys; nada liga job a (repo, SHA,
   ambiente).
4. **Fila volátil**: lista do Redis com `BRPOP` — se o agente cair entre tirar o job e terminar, o deploy
   some (o Prelo resolveu isso com fila no Postgres + lease + sweeper).
5. **Sem isolamento**: o Dockerfile do repositório é construído e executado no Docker do próprio host de
   controle, sem limite de CPU/memória/rede, sem healthcheck nem rollback (o container antigo é removido
   antes do novo subir).
6. **cloudflared por script com sudo** no host — difícil de automatizar com segurança.
7. **Entrada só por webhook de push**; não há GitHub Action com identidade verificável (OIDC).

**O que aproveitar:** estrutura API/agente em Go, verificação HMAC, modelo de deploy no Postgres, dashboard
e o provisionamento de cloudflared/Nginx como referência.

## 2. Work Control (`exotermo/work-control-mvp`)

**Estado real:** 6 commits no GitHub (último em 27/08/2026), apesar de o plano local dizer "sem Git".
- `apps/android`: Kotlin/Compose, Hilt, Retrofit/OkHttp, WebSocket; ~20 telas (Home, Tarefas, detalhe,
  Agente, Aprovação, Diff, Resultado, Terminal, Máquinas, Arquivos); testes unitários e de navegação.
- `services/api-go`: API própria com **schema próprio** (workspaces, members, projects, devices, agents,
  tasks, task_steps, executions, execution_events, approvals, artifacts, audit_logs), OIDC no servidor
  (Keycloak, JWKS, `/v1/me`), WebSocket de eventos de task, seed e simulador.
- `services/orchestrator-java` e `services/device-agent-go`: esqueletos.
- **Pendências declaradas no próprio checklist:** login Android (PKCE), autorização por membership/workspace
  (BOLA), cockpit funcional (criar tarefa, timeline, aprovação com risco), device agent, CI.

**Conflito principal:** o Work Control tem um **segundo modelo de domínio** (tasks, executions, approvals,
agents, devices) que duplica o que o Prelo já faz de verdade — inclusive aprovações e permissões. Manter os
dois significa dois orquestradores e duas políticas de autorização para o mesmo trabalho.

**Mapeamento para o Prelo:**

| Work Control | Prelo |
|---|---|
| workspace / workspace_members | instância + projetos/membros (`X-Project-Id`, RBAC ADMIN/OPERATOR) |
| projects | Project (+ Client) |
| tasks / task_steps / executions / execution_events | Task, Execution, turnos, pipeline, árvore de delegação |
| approvals | Approval (código curto, impacto, WhatsApp do dono) |
| agents | catálogo `GET /api/v1/agents` |
| devices / device_capabilities | Server (SSH, saúde) — device agent seria a evolução |
| artifacts | arquivos do projeto (cifrados) |
| WebSocket de eventos | **não existe no Prelo** (dashboard usa polling) |

## 3. Recomendação

- **BastionDeploy continua serviço próprio** (jobs, artefatos, versões, URL, rollback), mas **não cria uma
  segunda aprovação**: o pedido de deploy vira uma ferramenta de risco HIGH no Prelo (`request_deploy`) ou
  uma solicitação externa ao Prelo, e a decisão sai pelo mesmo fluxo de hoje (dashboard + "SIM <código>" no
  WhatsApp). O Prelo chama o BastionDeploy de volta com a decisão assinada, vinculada a repo + SHA +
  ambiente + destino.
- **Primeira fatia do BastionDeploy:** GitHub Action com OIDC (ou HMAC) → pedido persistido e idempotente por
  (repo, SHA, ambiente) → aprovação pelo Prelo → job liberado → checkout do **SHA exato**. Isolamento e
  cloudflared nas fatias seguintes.
- **Work Control vira cliente do Prelo**, não um segundo backend: aposentar tasks/executions/approvals
  próprios e mapear as telas Android para a API do Prelo.
- **Pré-requisitos no Prelo** para o app: login para cliente móvel (hoje o refresh é cookie de navegador) e
  eventos em tempo real (SSE/WebSocket) — ou polling na primeira versão.

## 4. Decisões que dependem do dono

1. Isolamento do deploy: **VM dedicada rodando containers** (mais simples, já dá separação do host de
   controle) ou **microVM** (Firecracker; isolamento mais forte, mais trabalho).
2. Login do app: **o próprio login do Prelo** (e-mail + senha + TOTP, com um fluxo próprio para app) ou manter
   o **OIDC/Keycloak** do Work Control e fazer o Prelo aceitar esses tokens.
3. A API Go do Work Control: **aposentar** (Android fala com o Prelo) ou **manter como fachada** (BFF) que só
   repassa ao Prelo.
