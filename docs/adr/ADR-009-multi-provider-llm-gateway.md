# ADR-009 — Gateway LLM multi-provider

**Status:** Aceito

## Contexto

O núcleo não pode depender de SDK ou credencial de fornecedor.

## Decisão

Gateway separado possui `LLMProvider`, `ModelRouter` e contrato `/api/v1/llm/chat`. A V1 habilita apenas `MockProvider` e `mock-echo`.

## Alternativas

Chamar Anthropic/OpenAI diretamente pelo Hermes foi descartado.

## Consequências

Novos adaptadores entram no Gateway; o roteamento por custo/capacidade permanece futuro.

## Implicações de segurança

Credenciais de providers pertencem exclusivamente ao Gateway e sua configuração externa.

## Evolução futura

Adicionar Anthropic, OpenAI-compatible e remoto/local atrás de `LLMProvider`.
