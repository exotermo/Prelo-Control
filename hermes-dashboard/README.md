# hermes-dashboard

SPA de administração do Hermes (`hermes-app-go`) — mesmo padrão do `messager-dashboard`
(React + Vite + TypeScript, repositório separado, consome a API direto do browser).

## Telas

- **Tasks**: criar, listar, executar, e ver o detalhe de uma task — status, resultado da
  execução, **trace turno a turno** (cada chamada ao Gateway e cada chamada de ferramenta, na
  ordem em que aconteceram) e a **árvore de delegação** (Fase C — sub-tasks criadas por
  `delegate_to_agent`, recursivamente).
- **Aprovações**: fila de `ApprovalRequest` pendentes (ferramentas de risco moderado/alto,
  etapa 8/ADR-004) — aprovar ou negar, com o texto do `scope` explicando exatamente o que a
  ferramenta pediu para rodar.

## Limitações conhecidas

- **`hermes-go` não tem autenticação hoje** (diferente do `messaging-core`, que tem OAuth2) —
  este dashboard funciona sem login. Está OK para localhost; expor além disso precisa primeiro
  de um gate de auth no `hermes-go` (ver ADR-014, "evolução futura" — deliberadamente fora de
  escopo desta etapa).
- A lista de tasks (`GET /api/v1/tasks`) só mostra tasks de topo (sem pai) — uma sub-task
  delegada aparece na árvore de delegação da task pai, não na lista principal.
- A busca de execução (`GET /api/v1/tasks/{id}/executions/latest`) assume a relação 1:1
  Task↔Execution que existe hoje — se isso mudar no futuro (uma task com múltiplas execuções),
  esse endpoint e esta tela precisam de revisão.
- Sem polling em tempo real via WebSocket/SSE — a tela de detalhe da task e a fila de aprovações
  atualizam por polling simples (2.5s/4s).

## Como rodar

### Docker

```bash
docker compose up -d --build
# abre em http://127.0.0.1:5176
```

`VITE_HERMES_URL` (build arg / env) aponta para o `hermes-go` — default
`http://127.0.0.1:8082`, que já é a porta publicada pelo `compose.yaml` de
`~/projects/hermes e renato`.

### Dev local

```bash
cp .env.example .env   # ajuste VITE_HERMES_URL se necessário
npm install
npm run dev
```

Certifique-se de que o `hermes-go` está rodando (`docker compose up -d hermes-go` no repositório
principal) — o CORS já está liberado lá (`internal/platform/cors.go`), de forma deliberadamente
permissiva (`Access-Control-Allow-Origin: *`, sem credenciais) já que `hermes-go` não tem
autenticação nem sessão para proteger hoje.
