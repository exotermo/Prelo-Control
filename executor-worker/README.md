# Executor isolado — worker rootless e orçamento de recursos

Este serviço não contém IA nem acesso ao host do Prelo. Ele autentica o executor, envia heartbeat
e, quando explicitamente habilitado, retira jobs aprovados e executa operações estruturadas no
Podman. Não o adicione ao compose.yaml da máquina principal.

Um ADMIN global registra um worker com POST /api/v1/executor-workers (name, projectId,
imageDigest no formato sha256:<64 hex>). A resposta entrega token uma única vez. Provisione-o
somente no usuário restrito do host executor; não o coloque em Git, no APK, na imagem ou em logs.
GET /api/v1/executor-workers lista o cadastro sem token; DELETE /{workerId} revoga.

Configure PRELO_BASE_URL com a origem HTTPS do Prelo e PRELO_EXECUTOR_TOKEN no usuário restrito
do host executor. Por padrão, go run . só envia heartbeat. Para habilitar execução, defina
PRELO_WORKER_ENABLE_RUNTIME=true e PRELO_WORKER_IMAGE com imagem existente, fixada por digest.
O Prelo também exige PRELO_EXECUTOR_ENABLED=true e armazenamento de arquivos configurado. O
serviço sai quando o Prelo rejeita a credencial; falha de rede não libera operação nenhuma.

O runtime exige cgroups v2 e usuário não root. Confirme no host com:

    stat -fc 'cgroups: %T' /sys/fs/cgroup

A saída esperada é cgroup2fs. Configure PRELO_WORKER_DATA_DIR=/mnt/storage/prelo-executor para
manter o armazenamento de imagens do Podman fora da raiz do sistema; o diretório precisa ser
gravável pelo usuário restrito. A sonda mede MemAvailable, limites e consumo do cgroup do worker,
CPU, PIDs e espaço disponível no volume.

O perfil inicial é conservador: reserva 1,5 GiB de memória, 1 CPU e 5 GiB de disco para o host;
cada container recebe limite de 256 MiB, 0,5 CPU e 64 PIDs. O worker processa um job por vez.
Esses valores são configuráveis pelo ambiente e precisam ser ajustados após medir CasaOS, backups
e Prelo em operação. Se a sonda falhar ou não houver recursos suficientes, o worker não faz claim.
O job aprovado permanece na fila e o motivo é registrado localmente. O cálculo de slots ainda não
conta workspaces existentes, que podem sobreviver a um job enquanto a task continua; por isso não
é uma garantia de concorrência máxima. O dashboard ainda não recebe o motivo nem mostra estimativas:
a API de capacidade, reconciliação de workspaces e estimativa está registrada como proposta P-5.

As APIs de pedido, aprovação, claim, confirmação atual, resultado e publicação estão implementadas
no Prelo. Apenas ADMIN global ou PROJECT_ADMIN pode aprovar; sessão web/app exige step-up quando
aplicável. O fluxo do modelo cria pedidos vinculados a tool calls e aprovações Prelo; após o
resultado terminal do worker, a task é retomada. A resposta do heartbeat nunca é autorização de
execução.

## Isolamento do container

internal/runtime/Podman monta um container por executionId com imagem fixada por digest,
--pull=never, rede desligada, raiz somente leitura, usuário sem privilégios, capacidades removidas
e limites de CPU/memória/PIDs. O workspace permanece em tmpfs; não há bind mount de diretórios do
host. O armazenamento de imagens é apontado explicitamente ao diretório PRELO_WORKER_DATA_DIR.
O Dockerfile.workspace gera uma imagem mínima que contém só workspace-helper. O helper aceita
apenas list, read, mkdir e create sobre caminhos relativos, com limites de bytes e proteção de raiz
via os.Root. O adaptador não recebe comandos arbitrários do modelo.

O gerenciador calcula vagas teóricas pela menor capacidade entre memória, CPU, disco, PIDs e teto
configurado. O worker continua executando um job por vez. Concorrência dinâmica fica bloqueada
até o contrato P-5 permitir adiar claims sem prender jobs em lease, informar o estado ao dashboard
e reconciliar containers quando tasks terminarem.

O orçamento de disco é uma reserva de admissão sobre o volume do graphroot, não uma quota física
por container. O workspace atual fica limitado pelo tmpfs de 64 MiB (e /tmp a 16 MiB); o graphroot
é monitorado por espaço livre. Uma quota individual de disco depende de suporte do filesystem e
será validada no host antes de aumentar a concorrência.

Para usar a imagem no host de validação, construa, inspecione e pré-carregue a imagem, obtenha seu
digest e registre-o no Prelo. Antes de habilitar runtime, valide o usuário rootless, o diretório
dedicado e as quotas. Os testes locais cobrem o cálculo de capacidade, a construção dos comandos e
a segurança das operações de arquivo; não provam o isolamento real do kernel nem que o host respeita
as quotas em execução.
