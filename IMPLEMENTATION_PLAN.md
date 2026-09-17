# Plano incremental de implementação

Este plano começa somente após aprovação da arquitetura e das decisões abertas de plataforma.

| Etapa | Entregável pequeno | Critério de aceitação |
|---|---|---|
| 0 | Bootstrap escolhido, formatter, lint e teste de fumaça | build e teste vazios passam em ambiente limpo |
| 1 | Tipos de domínio: IDs, Task, Directive, AgentDefinition, risco/permission | regras de transição e precedência cobertas por testes unitários |
| 2 | Portas de repositório e implementação SQLite com migrações | criar/reabrir task e directive preserva dados e versões |
| 3 | Casos de uso de conversa e task + eventos locais | intenção cria task e publica sequência verificável de eventos |
| 4 | ContextResolver com fontes manuais e ContextSnapshot | somente itens selecionados entram no snapshot com proveniência |
| 5 | LanguageModel port + um adaptador configurável/mock | resposta estruturada, timeout e erro tipado testados sem provider real |
| 6 | GeneralAssistantAgent e TaskOrchestrator linear | task simples percorre READY → RUNNING → COMPLETED/FAILED e possui trace |
| 7 | ToolRegistry, ferramenta read-only de demonstração e PermissionPolicy | ação bloqueada/permitida conforme perfil, toda chamada é auditada |
| 8 | Approval workflow | risco moderado não executa antes de aprovação válida e expira corretamente |
| 9 | UI mínima de conversa/task em MVVM+MVI | intents, states, effects e loading/erro observáveis em testes |
| 10 | Logs estruturados, métricas locais e tela/detalhe de execução | trace correlaciona task, agent, modelo e ferramenta |
| 11 | Hardening e documentação operacional | redaction, configuração sem secrets no repositório e testes de regressão |

Fora da V1: pesquisa e oportunidades, scheduler/briefing, agentes de código, ExoDeploy, homelab, educação adaptativa, finanças, economia e knowledge graph. Cada um inicia como um ADR e um vertical slice independente, usando as portas do núcleo.
