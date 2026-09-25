# Notas do ambiente de desenvolvimento (host do desenvolvedor)

Este arquivo registra problemas que pertencem **à máquina de desenvolvimento**, não ao projeto.
Nada aqui deve ser replicado no servidor que executa o Hermes.

## Build Docker sem acesso a `proxy.golang.org` (DNS do BuildKit)

- **Sintoma**: `docker compose build hermes-go` falha em `RUN go mod download` com
  `lookup proxy.golang.org on 1.0.0.1:53: i/o timeout`.
- **Escopo**: apenas a workstation de desenvolvimento. O `curl` do próprio host e um `docker run`
  comum resolvem o domínio normalmente; só o sandbox de build usa o resolvedor `1.0.0.1`, que
  expira.
- **Por que isso apareceu**: `hermes-app-go/vendor/` deliberadamente **não** é versionado, então o
  build baixa os módulos (`go mod download`).
- **Contorno aplicado (somente dev)**: `network: host` em `services.hermes-go.build` no
  `compose.yaml`. Com isso o build usa o DNS do host e conclui em ~30 s.
- **Correção de causa (fora do repositório)**: fixar o DNS do daemon Docker na máquina afetada,
  em `/etc/docker/daemon.json` (`{"dns": ["<resolvedor que funciona>"]}`) e reiniciar o Docker.
- **No servidor de produção**: remover a linha `network: host` do `compose.yaml` se o build lá
  alcançar `proxy.golang.org` normalmente (o esperado). `network: host` no build reduz o
  isolamento de rede do sandbox e não deve ser mantido sem necessidade.
