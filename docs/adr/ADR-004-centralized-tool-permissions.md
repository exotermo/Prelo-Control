# ADR-004 — Permissões centralizadas e aprovação limitada

**Status:** Proposto  
**Contexto:** Ferramentas podem alterar infraestrutura ou dados, e agentes não podem decidir sua própria autorização.

**Decisão:** Todas as invocações passam por `PermissionPolicy` e `ExecutionEngine`. Aprovações são vinculadas a ação, escopo e expiração; risco moderado ou superior requer confirmação explícita salvo política futura igualmente explícita.

**Consequências:** Mais segurança e auditoria; há uma etapa adicional na UX. Providers de LLM nunca executam comandos diretamente.
