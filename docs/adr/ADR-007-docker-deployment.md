# ADR-007 — Docker Compose para deployment inicial

**Status:** Aceito

## Contexto

Prelo precisa ser executável no `maquiavel` sem introduzir Kubernetes ou operação distribuída.

## Decisão

Usar Compose com imagens multi-stage Java 21, containers não-root, volume PostgreSQL e health checks.

## Alternativas

Instalação direta no host e Kubernetes foram descartados para a fundação.

## Consequências

O deploy é reproduzível e simples; atualização e rollback de imagens continuam responsabilidades operacionais futuras.

## Implicações de segurança

Sem modo privilegiado, Docker socket, host networking ou mounts do host.

## Evolução futura

Pode migrar para um orquestrador quando houver requisitos reais de escala.
