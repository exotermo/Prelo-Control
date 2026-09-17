# ADR-006 — SQLite local-first e eventos de auditoria

**Status:** Proposto  
**Contexto:** A V1 necessita persistência, histórico e rastreabilidade, mas não tem requisito de escala distribuída.

**Decisão:** Persistir agregados e eventos sanitizados em SQLite com migrações; usar logs estruturados locais e IDs correlacionados. Dados brutos/sensíveis não são gravados por padrão.

**Consequências:** Instalação simples e investigação suficiente para V1. Sincronização e PostgreSQL permanecem decisões futuras atrás de portas de repositório.
