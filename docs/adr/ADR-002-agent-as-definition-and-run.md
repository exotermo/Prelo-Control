# ADR-002 — Agentes como definição e execução rastreável

**Status:** Proposto  
**Contexto:** Papéis especializados precisam de contrato, política e auditoria, sem depender da UI ou de um provider de modelo.

**Decisão:** Modelar `AgentDefinition` (configuração/política) separadamente de `AgentRun` (execução concreta). O orquestrador seleciona agentes por capacidade e valida saídas estruturadas.

**Consequências:** Agentes são configuráveis e testáveis; exige schemas e versionamento de definições. A V1 terá poucos agentes e fluxo linear.
