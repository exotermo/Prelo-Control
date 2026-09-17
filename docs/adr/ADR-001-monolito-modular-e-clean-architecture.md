# ADR-001 — Monólito modular com dependências para o domínio

**Status:** Proposto  
**Contexto:** O projeto começa vazio, mas precisa evoluir sem acoplar agentes, UI e integrações.

**Decisão:** Iniciar como monólito modular com camadas presentation, application, domain e infrastructure. O domínio declara portas; a infraestrutura as implementa.

**Consequências:** Desenvolvimento e depuração locais simples, sem custo de rede ou operação distribuída. Extração futura exige fronteiras mantidas; microserviços não fazem parte da V1.
