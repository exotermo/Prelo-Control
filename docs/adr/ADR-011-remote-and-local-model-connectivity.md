# ADR-011 — Conectividade com modelos locais e remotos

**Status:** Aceito

## Contexto

Modelos podem estar em APIs externas, outra máquina ou infraestrutura própria; `maquiavel` não é assumido como host de GPU.

## Decisão

Somente o Gateway terá egress e futuros adaptadores devem usar endpoints explicitamente configurados, autenticados e com allowlist/política própria.

## Alternativas

Executar modelos dentro do Hermes ou conceder rede ampla ao host foram descartados.

## Consequências

Integrações locais/remotas são adapters independentes e podem falhar isoladamente.

## Implicações de segurança

Sem exposição de rede do host por padrão; comunicação remota exige autenticação e alvo explícito.

## Evolução futura

Adicionar provider OpenAI-compatible para Ollama/vLLM remoto, incluindo timeout, TLS e budgets.
