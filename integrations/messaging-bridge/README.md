# prelo-messaging-bridge

> **⚠️ Antes de conectar um número de WhatsApp real: por padrão este serviço NÃO responde a
> ninguém.** Uma resposta automática só é possível quando o operador configura
> `BRIDGE_AUTO_REPLY_ENABLED=true` **e** mantém a chave runtime em modo retomado via
> `POST /admin/resume`. Para parar imediatamente, use `POST /admin/pause` — não é necessário
> derrubar o container. Esta proteção é uma segunda camada independente da allowlist do core.

Serviço conector que liga o gateway de mensageria (`messager-interface`) ao Prelo
(`prelo-core`): recebe eventos de mensagem recebida via WhatsApp, manda pro Prelo
decidir uma resposta (como uma `Task` de agente), e envia a resposta de volta pelo
gateway.

```text
WhatsApp → messaging-core → callback assinado (inbound_message) → prelo-messaging-bridge
  → cria Task no prelo-core → executa → poll até COMPLETED/FAILED
  → resultado → POST /api/v1/messages no messaging-core → volta pro WhatsApp
```

Nenhum dos outros dois serviços foi editado para isso — a integração é só HTTP, pelos
contratos públicos de cada um.

## Como rodar do zero

```bash
docker network create prelo-shared  # uma vez; ignore "already exists" se ela já existir
cp .env.example .env
# preencha:
#   MESSAGING_CORE_URL, MESSAGING_CORE_CLIENT_ID, MESSAGING_CORE_CLIENT_SECRET
#     (credenciais de um tenant do messaging-core que este serviço vai usar pra
#     registrar o callback e enviar respostas)
#   PRELO_CORE_URL (base URL do prelo-core)
#   CALLBACK_SIGNING_SECRET (preenchido DEPOIS do passo de registro abaixo)
#   BRIDGE_ADMIN_TOKEN (segredo aleatório para os endpoints administrativos)
# Mantenha BRIDGE_AUTO_REPLY_ENABLED=false até revisar e liberar contatos conscientemente.
docker compose up -d --build
curl http://127.0.0.1:8095/actuator/health
```

### Controle de respostas automáticas (obrigatório)

`BRIDGE_AUTO_REPLY_ENABLED` tem default `false`: sem opt-in explícito o webhook assinado ainda
recebe `202 Accepted`, mas o bridge não chama Prelo nem `messaging-core`. Mesmo com a flag em
`true`, o operador pode interromper ou retomar em runtime com o padrão `X-Admin-Token`:

```bash
curl -X POST http://127.0.0.1:8095/admin/pause -H "X-Admin-Token: $BRIDGE_ADMIN_TOKEN"
curl -X POST http://127.0.0.1:8095/admin/resume -H "X-Admin-Token: $BRIDGE_ADMIN_TOKEN"
```

Os interruptores ficam em série: `resume` não sobrepõe a flag estática desligada e `pause`
sempre vence uma flag estática ligada. Cada resposta aceita por `POST /api/v1/messages` emite
`auto_reply_sent`, com hashes curtos do contato/resposta, `channelIdentityId` e `taskId`; texto
e telefone em claro não são registrados.

### Registrar o callback (uma vez, depois do primeiro boot)

```bash
CLIENT_ID=... CLIENT_SECRET=... \
BRIDGE_URL=http://prelo-messaging-bridge:8095 \
MESSAGING_CORE_URL=http://127.0.0.1:8090 \
./register-callback.sh
```

Isso registra `POST {BRIDGE_URL}/webhooks/messaging-core` como destino de callback no
messaging-core e imprime o `secret` retornado (só aparece uma vez). Copie esse valor pra
`CALLBACK_SIGNING_SECRET` no `.env` e reinicie o serviço (`docker compose up -d`).

## Endpoint interno

```
POST /webhooks/messaging-core
Header: X-Signature: sha256=<hmac-sha256 hex do corpo>
Body: {"eventType":"inbound_message","channelIdentityId":"UUID","conversationId":"UUID",
       "messageId":"UUID","externalMessageId":"string","from":"string","text":"string",
       "receivedAt":"ISO-8601"}
```

A assinatura é verificada (`MessageDigest.isEqual`, tempo constante) antes de qualquer
processamento — sem ela ou com ela inválida, `401` e nada é processado. Responde `202`
imediatamente (o processamento real — chamar o Prelo, esperar a execução, mandar a
resposta — acontece assíncrono via `@Async`), porque o `CallbackWorker` do messaging-core
tem orçamento de retry limitado e uma resposta lenta aqui só queimaria essas tentativas à
toa.

## Decisões

- **Sem persistência própria**: o serviço não guarda estado — se cair no meio do
  processamento de um evento, esse evento se perde (não há fila/retry neste lado). Escopo
  aceito nesta entrega; um evento perdido não trava nada a montante (o messaging-core já
  persistiu a `InboundMessage` de qualquer forma).
- **Timeout de execução do Prelo**: 30s de poll (`POLL_INTERVAL` 500ms). Se a execução não
  terminar (`COMPLETED`/`FAILED`) nesse prazo, nenhuma resposta é enviada — só logado. Não
  há mensagem de fallback ("não consegui responder agora") nesta entrega.
- **Autenticação do `prelo-core`**: o `PreloCoreClient` usa `client_credentials` no
  `messaging-core` e envia o bearer token nas chamadas de criação, execução e consulta.
  A integração precisa incluir pelo menos `tasks:create`, `tasks:read` e `tasks:execute`.
  Configure `PRELO_API_JWT_SECRET` no `prelo-core` com o mesmo segredo usado para
  assinar os tokens do `messaging-core`; nunca use o segredo do gateway LLM.

## Rede compartilhada entre os três `docker-compose.yml`

Este serviço, `messaging-core` (repo `messager-interface`) e `prelo-core` (repo
`prelo e renato`) vivem em três projetos Docker Compose diferentes, sem rede
compartilhada por padrão. Pro `messaging-core` conseguir entregar o callback aqui, e pra
este serviço conseguir chamar o `prelo-core`, os três precisam estar numa rede Docker
comum. Os compose deste projeto e do `messager-interface` já estão preparados; crie a rede
antes do primeiro boot:

```bash
docker network create prelo-shared
```

Cada lado se anexa a essa rede pelo seu próprio compose:

- Ponte: o `docker-compose.yml` daqui já declara e anexa `prelo-shared` (`external: true`).
- Prelo: `prelo-core` já está anexado no `compose.yaml` da raiz deste repositório.
- messager: o serviço **não** conhece o Prelo e não declara essa rede. Quem o consome se
  anexa com um override, mantido aqui: `compose.messager-link.yaml`. Suba o messager assim:

```bash
docker compose \
  -f <caminho-do-messager>/docker-compose.yml \
  -f integrations/messaging-bridge/compose.messager-link.yaml \
  up -d
```

Sem o override, o `messaging-core` sobe normalmente, mas não consegue entregar callbacks
para a ponte nem ser alcançado por ela pelo nome `messaging-core`.

Depois disso, `MESSAGING_CORE_URL=http://messaging-core:8090` e
`PRELO_CORE_URL=http://prelo-core:<porta>` (nomes de serviço via DNS do Docker) passam
a resolver de verdade entre os três compose.

## Testes

```bash
mvn verify
```

- `HmacVerifierTest`: assinatura válida aceita, inválida/ausente/com secret errado rejeita.
- `BridgeFlowIT`: fluxo completo (webhook assinado → Prelo mockado → messaging-core
  mockado) via `com.sun.net.httpserver.HttpServer` local — sem Docker, sem dependências
  externas.

## Estado atual

Webhook + verificação HMAC v2 + proteção durável contra replay + cliente pro Prelo (cria task,
executa, faz poll) + cliente pro messaging-core (token client_credentials cacheado, envio de
mensagem) implementados. O bridge exige `X-Webhook-Timestamp`, `X-Webhook-Id` e a assinatura
`sha256=HMAC(secret, timestamp + "." + rawBody)` por padrão. IDs já aceitos são persistidos em
`webhook_replays` e uma constraint única impede processamento concorrente ou após restart.
O modo body-only v1 só deve ser habilitado temporariamente com
`BRIDGE_ALLOW_LEGACY_CALLBACK_SIGNATURES=true` durante a migração.

## Limitações conhecidas

- Só processa o evento textual individual `inbound_message`; mídia e grupos estão fora de escopo.
- O callback é persistido antes do ACK, mas o processamento assíncrono ainda depende do worker
  do bridge; observar/reprocessar falhas operacionais continua sendo uma tarefa de hardening.
- O core despacha callbacks e outbound por polling Postgres a cada ~2s; resposta não é instantânea.
