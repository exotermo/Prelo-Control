# ADR-008 — Redes Docker isoladas

**Status:** Aceito

## Contexto

Banco, Prelo e Gateway têm necessidades de rede diferentes.

## Decisão

`prelo_internal` é rede Docker interna para Prelo, Gateway e PostgreSQL; `prelo_egress` conecta somente o Gateway à saída para providers futuros. Só Prelo publica `127.0.0.1:8080`.

## Alternativas

Rede única e portas LAN foram descartadas.

## Consequências

O banco é inacessível da LAN e Prelo não tem egress direto. Diagnóstico remoto usa SSH/tunnel.

## Implicações de segurança

Reduz movimento lateral e superfície de exposição, mas não substitui firewall/segurança do host.

## Evolução futura

Um reverse proxy/TLS pode ser adicionado como nova fronteira, não como publicação direta do Gateway.
