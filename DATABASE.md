# Database

PostgreSQL é a única persistência da topologia inicial e não publica porta no host. Os serviços usam a rede `hermes_internal`; somente Hermes e Gateway possuem credenciais de banco em runtime.

| Tabela | Dono lógico | Conteúdo | Não armazena |
|---|---|---|---|
| `hermes_llm_executions` | Hermes | request/task/agent IDs, modelo, provider, duração e status | prompt, resposta, JWT ou segredo |
| `gateway_audit_events` | LLM Gateway | evento de segurança/uso e correlação | chaves de provider, JWT e conteúdo LLM |

Cada serviço controla suas migrações Flyway em uma tabela de histórico distinta, preservando autonomia sem introduzir outro banco. O volume `postgres_data` conserva dados entre `down` e `up`; removê-lo é uma operação destrutiva e intencional.
