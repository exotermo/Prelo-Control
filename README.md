# Prelo Control

Prelo Control é uma plataforma de IA orientada a tarefas. Ela recebe uma solicitação, organiza o contexto, executa um agente por meio de modelos de linguagem e registra o resultado e os passos da execução. O projeto também oferece ferramentas com controle de permissões, aprovação humana para ações que exigem autorização e uma interface para acompanhar o trabalho.

O projeto se chamava **Hermes**; por isso o nome antigo ainda aparece no diretório do repositório, em código legado e em algumas migrações. O serviço principal atual é o `prelo-core`.

## Visão de negócio

O objetivo do Prelo é transformar pedidos em trabalho rastreável: cada tarefa tem um responsável lógico, um contexto delimitado e um histórico de execução que permite entender o resultado, acompanhar falhas e retomar o processamento. Projetos agrupam esse trabalho e seus recursos. O controle de capacidades e as aprovações humanas permitem que agentes usem ferramentas sem decidir a própria autorização.

A interface web atende à operação de tarefas, projetos, execuções e aprovações. A integração com mensageria permite receber solicitações e devolver respostas por canais externos, mantendo o núcleo do Prelo independente desses canais. Funcionalidades de domínios específicos, como oportunidades e finanças, aparecem em documentos de planejamento e não devem ser confundidas com o núcleo disponível hoje.

## Arquitetura

| Componente | Responsabilidade |
| --- | --- |
| [`prelo-core/`](prelo-core/) | Serviço principal em Go: API, projetos, tarefas, agentes, contexto, execuções assíncronas, ferramentas, permissões e aprovações. |
| [`llm-gateway/`](llm-gateway/) | Serviço Java que concentra as chamadas aos provedores de modelos e isola suas credenciais do núcleo. |
| [`prelo-dashboard/`](prelo-dashboard/) | Interface React/TypeScript para operar e acompanhar o Prelo. Possui implantação própria. |
| [`integrations/messaging-bridge/`](integrations/messaging-bridge/) | Ponte entre o Prelo e o sistema de mensageria externo; traduz eventos em tarefas e encaminha respostas. |
| PostgreSQL e Redis | Persistência de dados e jobs; Redis participa do processamento assíncrono. |
| [`legacy-java-app/`](legacy-java-app/) | Implementação Java anterior, mantida como referência e fora da subida padrão do Compose. |

Fluxo principal: uma chamada à API cria uma **Task**; ao solicitar sua execução, o `prelo-core` cria uma **Execution** e a processa de forma assíncrona. O agente recebe um recorte do contexto e acessa o modelo pelo `llm-gateway`. Pedidos de ferramenta passam pela política de permissões e podem aguardar aprovação humana. O resultado e os turnos são registrados para consulta e auditoria. A mensageria se conecta por uma ponte separada, sem incorporar a lógica do canal ao núcleo.

O `prelo-core` é o serviço padrão do [`compose.yaml`](compose.yaml). A API usa autenticação por token nas rotas de negócio; o Gateway é acessado por uma credencial de serviço distinta. Credenciais de provedores ficam no Gateway, não nos agentes.

## Dicionário do projeto

| Termo | Significado |
| --- | --- |
| **Project (projeto)** | Espaço de trabalho que agrupa tarefas e recursos relacionados. |
| **Task (tarefa)** | Unidade de trabalho criada a partir de uma solicitação; pode ter uma tarefa pai quando há delegação. |
| **AgentDefinition (definição de agente)** | Configuração da identidade, instruções e capacidades de um agente. |
| **Execution (execução)** | Processamento de uma tarefa, com estado e resultado próprios. |
| **ExecutionTurn (turno)** | Registro de uma etapa da execução, como chamada ao modelo, uso de ferramenta ou delegação. |
| **ContextSnapshot (retrato de contexto)** | Seleção de informações usada por uma execução, preservada para rastreabilidade. |
| **Tool (ferramenta)** | Ação disponibilizada ao agente por um catálogo controlado. |
| **Capability (capacidade)** | Permissão prevista na definição do agente para solicitar determinada ferramenta; ainda sujeita à política de autorização. |
| **PermissionPolicy (política de permissões)** | Regra aplicada pelo núcleo que permite, nega ou exige aprovação para uma chamada de ferramenta. |
| **ApprovalRequest (pedido de aprovação)** | Decisão humana vinculada a uma ação específica antes de sua execução. |
| **LLM Gateway** | Única porta do núcleo para os provedores de modelos de linguagem. |

## Como começar

Copie [`.env.example`](.env.example) para `.env`, preencha os valores obrigatórios indicados no arquivo e suba os serviços principais:

```bash
cp .env.example .env
docker compose up -d --build
curl http://127.0.0.1:8082/actuator/health
```

A API do núcleo fica em `127.0.0.1:8082`. As rotas `/api/v1/**` exigem autenticação; consulte [`API.md`](API.md) para contratos e escopos. O dashboard tem [instruções próprias](prelo-dashboard/README.md) de execução.

## Onde encontrar mais informações

- [Estado atual](CURRENT_STATE.md): serviço ativo, cutover e limitações conhecidas.
- [Arquitetura técnica](docs/PRELO_ArquiteturaTecnica.md): componentes, fluxos e decisões da implementação documentada.
- [ADRs](docs/adr/): decisões arquiteturais e seus motivos.
- [API](API.md): endpoints, respostas e requisitos de autorização.
- [Banco de dados](DATABASE.md): schemas e migrações.
- [Segurança](docs/SECURITY_HARDENING.md): autenticação e controles aplicados.
- [Plano de implementação](IMPLEMENTATION_PLAN.md) e [arquitetura proposta](ARCHITECTURE.md): evolução planejada; consulte o estado atual e o código para confirmar o que já está implementado.
- [Regras para agentes](AGENTS.md): limites e responsabilidades dos agentes e do Gateway.
