# ADR-003 — Contexto por snapshot e memória curada

**Status:** Proposto  
**Contexto:** Todo o histórico não cabe nem deve ser enviado a cada agente; registros passageiros não devem virar conhecimento permanente.

**Decisão:** Resolver contexto por tarefa em `ContextSnapshot` imutável e versionado; manter memória como entidade persistente com escopo, proveniência e confiança. Promoção para memória é explícita.

**Consequências:** Reproduzibilidade e privacidade melhores. Há custo de selecionar fontes e gerir retenção.
