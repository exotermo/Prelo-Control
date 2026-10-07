# Gerenciador de recursos para tasks e containers

## Objetivo

Dimensionar o trabalho do executor pela capacidade efetiva do host, preservar recursos para Debian,
CasaOS, backups e Prelo, e manter tasks sem perder pedidos quando a capacidade estiver ocupada.
O gerenciador nunca altera permissões, ferramentas autorizadas ou decisões de aprovação.

## Estado do host de validação

- Debian 13, kernel 6.12, CPU de 2 núcleos e aproximadamente 3,7 GiB de RAM.
- O host confirmou cgroups v2: statfs em /sys/fs/cgroup retorna cgroup2fs.
- O volume /mnt/storage tem espaço amplo segundo o inventário do dono; /var estava quase cheio.
- O runtime será rootless, sob usuário dedicado, com imagens em /mnt/storage/prelo-executor.
- O worker ainda não foi instalado nem conectado nesse host. Nenhum teste remoto foi executado.

## Fase 1 — Medir recursos e manter reservas

Implementado no worker:

- sonda de MemTotal/MemAvailable, cgroup v2 (memória, CPU quota, cpuset e PIDs), load average e
  espaço disponível no diretório de dados;
- cálculo conservador de vagas pelo menor orçamento entre memória, CPU, disco, PIDs e limite
  configurado;
- runtime rootless somente; erro de sonda impede claim;
- armazenamento Podman em diretório configurado, separado da raiz do sistema;
- limites de container configuráveis, com defaults conservadores: 256 MiB, 0,5 CPU e 64 PIDs;
- reserva inicial configurável de 1,5 GiB de memória, 1 CPU e 5 GiB de disco.

O worker executa um job por vez. O cálculo de admissão agora considera containers ativos rotulados
por executionId; a limpeza/reconciliação de workspaces após encerramento da task ainda depende da
operação de lifecycle descrita na P-5.

## Fase 2 — Perfis de recursos no servidor

Implementado no Prelo como catálogo declarativo `workspace-small-v1`: 256 MiB, 500 millicores,
1 GiB de disco estimado, 64 PIDs e 600 segundos. A Caixa de ferramentas mostra o perfil e informa
que ele ainda não é aplicado/confirmado pelo worker. O modelo não escolhe o perfil.

## Fase 3 — Cálculo dinâmico de concorrência

Implementado no worker como cálculo puro: `AssessWithActive` calcula vagas adicionais pelo mínimo
entre memória disponível após reserva, CPU permitida após reserva e pressão de carga, disco após
reserva, PIDs restantes e `PRELO_WORKER_MAX_CONTAINERS - containersAtivos`. O Podman expõe uma
consulta rootless dos containers ativos com o label `prelo.execution_id`; resposta malformada ou
erro do runtime não é interpretado como zero.

O claim recebe o perfil e o número de vagas, e a reserva distingue início de workspace de operação
em workspace ativo. A fila de espera não consome lease e recupera jobs quando o worker reporta
vaga. A extensão está documentada em P-5; ainda não foi promovida para `CONTRATOS.md`.

O orçamento de disco do perfil ainda é uma estimativa: o adaptador Podman não impõe quota por
container. Por isso, não aumente `PRELO_WORKER_MAX_CONTAINERS` acima de 1 antes de definir e validar
uma quota efetiva e o lifecycle/reconciliação dos workspaces. O default
continua sendo 1.

## Fase 4 — Espera, incapacidade estrutural e limite excedido

Implementado no protocolo local desta fatia:

- O worker envia snapshot recente de perfil, vagas atuais e máximas, containers ativos e IDs de
  execução no claim. A telemetria só decide a admissão na fila; não concede autorização.
- Sem vaga atual, o pedido aprovado fica `WAITING_FOR_CAPACITY`, sem lease. O próximo polling
  reavalia e o retorna a `READY` quando houver capacidade. Operações em workspaces já ativos
  continuam elegíveis mesmo quando não cabe iniciar outro container.
- Perfil incompatível ou sem capacidade estrutural no worker termina em `UNSUPPORTED_CAPACITY`;
  o Prelo registra a explicação e retoma a task com orientação para dividir o trabalho ou usar um
  host maior.
- OOM de memória do container encerra somente o container daquela execução e reporta `FAILED` com código
  `resource_limit_exceeded`; a job não volta à fila automaticamente. A suíte cobre a classificação
  e a remoção pelo nome/ID da própria execução.
- Resultados terminais guardam `task_notified_at`; se a retomada da task falhar, o próximo polling
  tenta notificá-la novamente sem executar a operação outra vez.

O limite de memória tem detecção via estado OOM do Podman. Limites de PIDs, CPU e disco ainda não têm
classificação uniforme de violação; o disco continua sem quota hard por container. O perfil atual
é único; por isso, não se deve interpretar `UNSUPPORTED_CAPACITY` como estimativa semântica do
tamanho do trabalho. O claim e os novos estados são uma extensão do
protocolo do executor, isolada de `CONTRATOS.md`; a implementação não altera as APIs de task comuns.

## Fase 5 — Estimar custo antes de criar task

Implementada parcialmente no endpoint `POST /api/v1/tasks/estimate` e no formulário de Tasks:

- tokens de entrada/saída e duração do Gateway são gravados por chamada, associados ao perfil do
  agente, tipo de task, faixa de contexto e requestId existente; amostras similares geram faixas
  p20/p50/p80;
- hardware exibe o perfil definido no Prelo e, quando disponível, a última capacidade reportada pelo
  worker; o orçamento por perfil é estimado, não equivale a uso real por execução;
- revisão/testes mostra faixa heurística separada, risco e fatores, sempre com confiança LOW porque
  não há eventos de duração humana nem histórico de arquivos afetados/validação.

A UI solicita a prévia após uma pausa curta na digitação. Falha da estimativa não bloqueia criar a
task. A estimativa não chama o modelo, não reserva capacidade e não representa preço monetário.
Faltam telemetria de pico de recursos reais por execução e contrato/eventos para registrar revisão e
testes humanos; estão documentados em P-5.

## Comparação da estimativa e estado de capacidade no dashboard

Implementado localmente:

- a task conserva no navegador uma cópia não autoritativa da estimativa feita antes da criação;
- o detalhe compara tokens e tempo estimados com os agregados reais devolvidos pelo Gateway;
- o detalhe mostra requests da task como aguardando aprovação, aguardando capacidade, incapazes neste
  worker ou interrompidos por OOM, com a explicação correspondente;
- a Caixa de ferramentas mantém a visão por projeto e indica que a telemetria de capacidade vem do
  claim do worker.

O dashboard não usa essa cópia para autorização ou execução. O uso real de CPU/memória/disco por task
e a duração humana de revisão/testes ainda não são medidos. Os agregados por turno acrescentados à
resposta de observabilidade estão registrados como P-6 e não alteram o protocolo do worker.

## Validação no host de teste

1. Confirmar espaço livre de /var, /mnt/storage e /srv.
2. Criar usuário de sistema sem sudo e sem acesso aos dados de backup.
3. Criar o diretório exclusivo do worker em /mnt/storage e garantir que somente esse usuário possa
   gravar nele.
4. Confirmar Podman rootless, cgroups v2, cgroup delegation e aplicação efetiva de memória/CPU/PIDs.
5. Começar com um job e limites padrão; observar RAM, CPU, disco, PIDs e serviços do CasaOS.
6. Testar falta de memória/disco, queda do Prelo, revogação e encerramento de task.
7. Só elevar concorrência após testes repetidos sem pressão crítica nem crescimento de workspaces
   órfãos.

## Critérios de segurança e conclusão

- Nenhuma task é descartada por falta temporária de recurso; fica aguardando e o usuário recebe o
  motivo no estado do pedido.
- Falha de telemetria impede novos claims.
- Cada container mantém limites rígidos, não recebe socket ou diretório de backup e só acessa o
  workspace da sua execution.
- O modelo não escolhe worker, imagem, limites, concorrência ou aprovação.
- Estimativas são faixas informativas, nunca orçamento garantido nem autorização.
- A validação no servidor não é equivalente à VM dedicada e não deve ser usada para workloads
  não confiáveis em produção.
