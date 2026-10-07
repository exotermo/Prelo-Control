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

### P-6 — Expor consumo agregado na observabilidade de execução
- Origem: Prelo Control / dashboard
- Status: proposta; extensão local implementada nesta fatia para comparação no dashboard (2026-10-07), contrato ainda não congelado
- Problema: o Prelo já registra tokens e duração por chamada do Gateway para estimativas, mas `GET /api/v1/tasks/{taskId}/executions/{executionId}/turns` não expõe esses agregados. Sem isso, o dashboard não consegue comparar a faixa apresentada antes do envio com o uso real após a execução.
- Contrato proposto: cada `LLM_CALL` em `GET …/turns` pode incluir `usage: {modelProfile, taskKind, estimatedContextTokens, inputTokens, outputTokens, durationMs}`. `estimatedContextTokens` é uma aproximação calculada pelo Prelo sobre o tamanho do contexto serializado; os demais contadores vêm da resposta do Gateway. O campo é omitido quando não há métricas. Não inclui prompt, resposta, custo monetário nem credenciais. Valores são telemetria, não autorização nem garantia de custo. Clientes antigos ignoram o campo.
- Alternativa sem mudar o Prelo: manter somente a estimativa anterior e não apresentar comparação com o consumo real.
- Impacto de segurança: somente acrescenta contadores agregados a uma rota de observabilidade já autorizada; `requestId` permanece a correlação não sensível; nenhum conteúdo novo é revelado.
- Prazo/necessidade: necessário para comparar estimativa e consumo real de tokens e duração no detalhe da task.

### P-2 — Aprovação administrativa de ação isolada por projeto
- Origem: executor-worker
- Status: aceita pelo dono (2026-10-06); implementação em andamento
- Problema: `approvals:decide` de OPERATOR hoje aprova ferramentas de risco; esse mecanismo não prova que quem autorizou um contêiner ou uma operação de arquivo é ADMIN global ou administrador do projeto. O papel de administrador de projeto ainda não existe. Reaproveitar a aprovação atual para liberar o worker ampliaria a autoridade do OPERATOR.
- Contrato proposto: novo tipo de pedido `EXECUTOR_ACTION`, criado internamente pelo Prelo com `projectId`, `taskId`, `executionId`, `toolCallId`, `workerId`, `imageDigest`, `operation` (`START_WORKSPACE|LIST|READ|MKDIR|CREATE`), argumentos exatos, `payloadHash`, versão da política e `expiresAt`. `GET /api/v1/projects/{projectId}/executor-requests` e `GET /api/v1/executor-requests/{id}` exigem leitura do projeto. `POST /api/v1/executor-requests/{id}/approve` e `/deny` exigem sessão humana ADMIN global ou futura função `PROJECT_ADMIN` associada ao projeto; aprovação de ação HIGH no app exige step-up/TOTP. A identidade do aprovador vem da sessão, nunca de `decidedBy` no corpo. Estados `PENDING|APPROVED|DENIED|EXPIRED`; decisão idempotente por ID/versão; erro `403` sem papel, `404` fora do projeto, `409` expirado/decidido/conflito de versão. Um pedido aprovado fixa o payload imutável e tem janela curta para início; mudança da política, worker ou projeto invalida o uso.
- Alternativa sem mudar o Prelo: manter o runtime desligado e executar somente testes com worker falso.
- Impacto de segurança: ADMIN global e futuro administrador do projeto passam a poder autorizar a operação exata; OPERATOR, worker e modelo continuam sem poder aprovar ou ampliar política.
- Prazo/necessidade: bloqueia a ligação da fila E2 e as operações E3.

### P-3 — Fila durável e confirmação de autorização pelo worker
- Origem: executor-worker
- Status: aceita pelo dono (2026-10-06); implementação em andamento
- Problema: o heartbeat atual só autentica o worker; não há maneira segura de reservar uma operação aprovada uma vez, recuperar uma queda ou confirmar a decisão imediatamente antes de executar.
- Contrato proposto: após P-2, o Prelo cria job durável vinculado 1:1 ao pedido aprovado. `POST /api/v1/executor-workers/jobs/claim` com bearer próprio do worker reserva atomicamente apenas job do projeto/worker/imagem da credencial e devolve `jobId`, `requestId`, IDs de projeto/task/execução, operação, argumentos, `payloadHash`, `leaseId`, `leaseUntil`. `GET /api/v1/executor-workers/jobs/{jobId}` devolve a autorização atual com todos os campos imutáveis, estado, prazo e lease; o worker compara tudo antes de iniciar. `POST /api/v1/executor-workers/jobs/{jobId}/result` exige a credencial e lease vigentes, registra `sequence` monotônico, estado e evidência limitada, com idempotência. `204` quando não há job; `401` credencial inválida; `403` projeto/worker divergente; `409` lease ou autorização vencidos; `429` limite; `503` falha do armazenamento. Rede, timeout, parse inválido, revogação ou política divergente recusam execução. Reentrega após crash não cria um segundo contêiner para a mesma execução; o worker reconcilia pelo ID estável.
- Alternativa sem mudar o Prelo: worker permanece somente no heartbeat, com adaptador Podman e confirmação falsa apenas em testes locais.
- Impacto de segurança: a credencial do worker ganha apenas claim/GET/result de jobs do seu projeto e não recebe scope humano ou endpoint de política. Nenhum job é liberado por evento/webhook sem GET atual.
- Prazo/necessidade: bloqueia ativação de E2 na VM.

### P-4 — Publicar arquivo da task nos Arquivos do projeto
- Origem: executor-worker
- Status: aceita pelo dono (2026-10-06); implementação em andamento
- Problema: o upload atual exige sessão humana, reduz o nome ao basename e não vincula arquivo à task/execução; o worker não deve receber token humano nem chave de cifragem.
- Contrato proposto: `PUT /api/v1/executor-workers/jobs/{jobId}/files/{fileId}` com bearer do worker, lease e fluxo binário limitado. O Prelo deriva `projectId`, `taskId`, `executionId`, caminho relativo e hash esperados do pedido aprovado; valida bytes/hash, rejeita symlink/caminho absoluto/`..`, aplica quota e cifra via `ProjectFileService`. `fileId` estável e chave `(jobId, relativePath)` tornam retry idempotente. A lista `GET /api/v1/projects/{projectId}/files` devolve `relativePath`, origem da task e ID da execução; a UI mostra diretórios virtuais. Respostas `201` criada, `200` retry idêntico, `409` colisão/hash divergente, `413` quota, `403` grant/lease inválidos. Resultado de CREATE só pode ser SUCCEEDED depois da publicação persistida.
- Alternativa sem mudar o Prelo: não anunciar criação de arquivo como concluída e manter E3 desabilitada.
- Impacto de segurança: worker envia bytes apenas ao projeto derivado do job; não escolhe projeto nem escreve no host principal. Arquivos continuam cifrados e sujeitos ao limite existente.
- Prazo/necessidade: bloqueia a validação da task que cria arquivo em `/projetos/{projectId}/arquivos`.

### P-5 — Capacidade do executor, espera por recursos e estimativa de task
- Origem: executor-worker
- Status: proposta; espera/limite estrutural/estouro de memória e estimativas de tokens/tempo implementados localmente sob orientação do dono (2026-10-07); telemetria real de hardware e revisão humana ainda pendentes
- Problema: o worker pode medir a capacidade local e evitar novos claims, mas o Prelo não recebe estado de capacidade nem distingue uma aprovação aguardando hardware de uma execução em andamento. A criação de task também não oferece estimativas de hardware, tokens, duração do modelo ou tempo humano de revisão/testes. O endpoint atual de claim não permite consultar a operação antes de reservar o job; por isso o worker não pode adiar com segurança apenas pedidos que exigiriam um novo container, preservando operações em workspaces já ativos.
- Contrato proposto:
  - O heartbeat autenticado do worker pode incluir um snapshot limitado de cgroupsVersion, memoryAvailableBytes, memoryLimitBytes, memoryCurrentBytes, cpuQuota, diskAvailableBytes, pidsAvailable, activeWorkspaces e observedAt. O Prelo armazena apenas a última observação e a expõe a administradores do projeto; valores enviados pelo worker são telemetria, nunca autorização.
  - A reserva de job deve ser atômica e permitir ao worker declarar capacidade disponível por perfil. O Prelo mantém jobs aprovados em WAITING_FOR_CAPACITY quando não houver capacidade, sem consumir lease nem perder aprovação; volta a READY quando o worker reportar capacidade suficiente. Um perfil acima da capacidade máxima do worker vira UNSUPPORTED_CAPACITY, com motivo legível e sem tentativa automática infinita. O worker continua confirmando estado e autorização atuais antes da execução.
  - A reserva precisa considerar `activeContainers` reconciliados pelo worker e retornar somente a vaga adicional calculada. Para um pedido `START_WORKSPACE`, a reserva só ocorre se `min(memória livre após reserva / memória do perfil, CPU permitida após reserva / CPU do perfil, disco livre após reserva / disco do perfil, PIDs livres / PIDs do perfil, maxContainers - activeContainers) >= 1`. Para operações de um workspace existente, a elegibilidade deve indicar o `executionId` e reutilizar a reserva daquele workspace, sem exigir uma vaga nova. A fila não deve consumir lease para pedidos que aguardam capacidade. Relatório de containers ativos é estado operacional não confiável para autorização; o Prelo ainda valida aprovação, worker, projeto, imagem e payload como hoje.
  - O claim recebe snapshot recente com o perfil, `availableSlots`, `maximumSlots`, `activeContainers`, `activeExecutionIds` e `observedAt`. Snapshot inválido/antigo é recusado sem claim. A máquina de estados dos jobs passa a incluir `WAITING_FOR_CAPACITY` (recuperável, sem lease) e `UNSUPPORTED_CAPACITY` (terminal neste worker); jobs aguardando voltam a `READY` após telemetria suficiente. `UNSUPPORTED_CAPACITY` completa o tool call com explicação para dividir a tarefa ou selecionar host mais capaz. OOM da execução resulta em `FAILED` com `code=resource_limit_exceeded`, encerra só o container daquela execução e não é reencaminhado automaticamente. Falhas terminais guardam confirmação de retomada para retry idempotente da notificação à task, sem repetir a operação.
  - O perfil é selecionado exclusivamente pelo Prelo e vinculado ao pedido aprovado. O envelope inicial `workspace-small-v1` fixa memória em 268435456 bytes, CPU em 500 millicores, disco em 1073741824 bytes, 64 PIDs, duração máxima de 600 segundos e concorrência máxima de uma task. A resposta de claim/consulta deve carregar `resourceProfileId` e os limites versionados; antes de iniciar o container, o worker valida que seu runtime consegue aplicar todos os limites e confirma o perfil no resultado. Perfil desconhecido, limite não aplicável ou diferença de versão falha fechado. O modelo, o cliente e a configuração do projeto não podem elevar esses valores. Até a negociação e o teste do protocolo, o catálogo exibido pelo Prelo é informativo e não atesta enforcement no worker.
  - O worker precisa reconciliar containers por executionId com o estado atual de task/execução no Prelo. Quando a task terminar, falhar, for cancelada ou expirar, o worker recebe ou consulta o estado terminal e encerra/remove o container e seu workspace temporário. A reconciliação deve ser idempotente e não aceitar ordem de encerramento do modelo como autorização. Sem essa operação, workspaces persistentes podem consumir a capacidade e deixar novos pedidos aprovados parados.
  - POST /api/v1/tasks/estimate aceita {description, agentId?, projectId?} e exige tasks:create com o mesmo escopo de projeto usado na criação. A resposta inclui perfil de recursos estimado, disponibilidade atual do worker, faixas min/esperado/max de tokens de entrada/saída por modelo, duração da execução do modelo e minutos humanos estimados para revisão/testes, além de confidence e observedAt. Valores vêm de telemetria histórica e configuração atual do projeto; sem amostra suficiente o servidor retorna confidence LOW e faixas amplas. O endpoint não chama o modelo e não reserva recursos.
  - Para atribuir consumo de tokens a tasks, o Gateway deve persistir taskId, agentId e requestId já propagados como metadados pelo Prelo, associados aos tokens e duração retornados. A estimativa usa esses dados apenas agregados e não registra prompts ou respostas para fins de telemetria.
  - A estimativa de revisão/testes humanos exige eventos explícitos de início e fim ou feedback de duração associado à task. Sem esses dados, deve exibir LOW confidence e não apresentar a faixa como compromisso.
  - A implementação local atual registra uso agregado de tokens e duração por chamada do Gateway, perfil do agente, tipo de task e faixa de contexto; não copia conteúdo de prompt para a nova telemetria. `POST /api/v1/tasks/estimate` devolve faixas separadas e níveis de confiança. A interface existente não oferece eventos de revisão humana nem o worker reporta pico de memória/CPU/disco por task; essas estimativas permanecem LOW e heurísticas até contrato e telemetria correspondentes serem aceitos.
  - POST /api/v1/tasks continua criando a task conforme o contrato atual. A estimativa é informativa; a autorização, a aprovação e a admissão efetiva continuam no servidor e no worker. A resposta da execução pode incluir consumo real para calibrar as estimativas.
  - A resposta de estimativa não expõe segredo nem preço de provider sem tabela de tarifas configurada pelo administrador. Se custo monetário for incluído futuramente, deve ser faixa e identificar a versão da tabela usada.
- Alternativa sem mudar o Prelo: manter o worker limitado a um claim por vez, registrar telemetria somente em log local e não mostrar estimativa de custo/capacidade na criação da task. Isso não fornece motivo de espera no dashboard nem estimativa baseada em dados reais.
- Impacto de segurança: o worker só informa telemetria de si próprio; telemetria nunca concede permissão. O modelo não escolhe perfil, limites, worker ou concorrência. A aprovação Prelo permanece necessária e a capacidade não amplia scopes. A estimativa não reserva nem inicia execução.
- Prazo/necessidade: bloqueia concorrência dinâmica segura, estado de espera visível no dashboard e estimativas confiáveis na criação da task.

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
