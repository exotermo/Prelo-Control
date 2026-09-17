# ADR-005 — Porta de LLM neutra a provider

**Status:** Proposto  
**Contexto:** O provedor, modelo e custo podem mudar; o domínio não deve conhecer SDKs externos.

**Decisão:** Usar uma porta `LanguageModel` com prompt compilado, configuração e resposta estruturada, implementada por adaptadores de infraestrutura.

**Consequências:** Testes usam fake/mock e troca de provider não invade casos de uso. A normalização inicial de capacidades de modelos deve permanecer mínima.
