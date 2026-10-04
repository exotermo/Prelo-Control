# ADR-015 — Prelo como plano de controle; BastionDeploy e Work Control como produtos integrados

**Status:** Aceito (2026-10-04)

## Contexto

O Prelo já orquestra agentes, ferramentas e aprovações (ADR-004, ADR-014, Fase T: aprovação do dono pelo
WhatsApp). Dois produtos vizinhos cresceram em paralelo: o **Work Control** (Android + API Go própria +
Keycloak), que duplica tarefas, execuções, aprovações e permissões; e o **BastionDeploy** (API Go + Redis +
agente no próprio host), que implanta sem aprovação e executa código de cliente no host de controle.

## Decisão

1. **O Prelo é o plano de controle e a única autoridade de autorização** de ações de risco. Não executa
   workloads de clientes.
2. **O BastionDeploy é um produto separado, executor de deploys**. Pede autorização ao Prelo por contrato
   (`docs/integracoes/action-requests.md`), executa somente o conteúdo aprovado (commit SHA + destino) e
   reporta o resultado. Não tem aprovação própria nem acesso ao banco do Prelo.
3. **Workloads rodam fora da VM do Prelo**: worker em VM (KVM/libvirt; Proxmox quando houver host
   dedicado) com Docker isolado (rootless/userns, sem `docker.sock`, sem bind mounts, sem privilégios, rede
   por cliente, limites de recurso). O worker disca para o Bastion; não alcança a rede do controle.
4. **O Work Control é outro frontend do Prelo**, com a identidade do Prelo (`docs/integracoes/sessao-mobile.md`).
   A API Go, o Keycloak, o orquestrador Java e o device agent do Work Control são aposentados; um BFF só
   entra com necessidade concreta, sem banco de domínio e sem autorização própria.
5. **Kubernetes não entra agora**: reavaliar com ≥ 3 workers ou necessidade real de autoscaling.
6. **Mudança de contrato não é silenciosa**: quem precisa de algo novo do Prelo registra em
   `docs/integracoes/PROPOSTAS.md`.

## Consequências

- Uma fonte de verdade para identidade, projetos, tarefas e aprovações; auditoria de decisões no Prelo e de
  execução no Bastion, ligadas por `actionRequestId` e `payloadHash`.
- O Prelo precisa ganhar: `/me` + workspace, sessão mobile, pedidos de ação externa com webhook de decisão
  e relato de resultado (lacunas G1–G11 em `docs/CONTRATOS.md`).
- O Bastion precisa: SHA imutável, fila durável (Postgres), worker isolado, blue/green com rollback, testes.
- Hardware: a VM do Prelo (4 GB) não comporta workers; eles precisam de outro host.
