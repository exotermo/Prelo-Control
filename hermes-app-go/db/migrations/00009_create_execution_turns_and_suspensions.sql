-- +goose Up
-- Fase A do plano "loop de ferramentas / orquestrador / observabilidade": ledger apend-only por
-- execução (execution_turns) e o mecanismo genérico de pausar/retomar (execution_suspensions).
-- Nenhuma mudança em execution_jobs.status precisa de migration — é VARCHAR(32) sem CHECK
-- constraint, então o novo valor 'AWAITING_RESUME' já é aceito; RequeueOrphaned nunca o toca
-- porque sua cláusula é uma lista explícita ('CLAIMED','RUNNING'), não uma exclusão.
CREATE TABLE execution_turns (
    id             UUID PRIMARY KEY,
    execution_id   UUID NOT NULL REFERENCES task_executions(id),
    turn_number    INT NOT NULL,
    kind           VARCHAR(20) NOT NULL,
    request_id     VARCHAR(200) NOT NULL,
    input          TEXT NOT NULL,
    output         TEXT,
    error          VARCHAR(1000),
    started_at     TIMESTAMPTZ NOT NULL,
    completed_at   TIMESTAMPTZ
);

-- (execution_id, turn_number) is the ordering key; request_id is the idempotency key — both
-- unique, for the same reason (a retried turn must collide, not duplicate).
CREATE UNIQUE INDEX execution_turns_execution_turn_uk ON execution_turns(execution_id, turn_number);
CREATE UNIQUE INDEX execution_turns_request_id_uk ON execution_turns(request_id);

CREATE TABLE execution_suspensions (
    id             UUID PRIMARY KEY,
    execution_id   UUID NOT NULL REFERENCES task_executions(id),
    reason         VARCHAR(20) NOT NULL,
    resume_key     VARCHAR(200) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL,
    resolved_at    TIMESTAMPTZ
);

-- Lookup path used by resolvers (approval decided, sub-task completed): "is anything waiting on
-- this key, still unresolved?" — partial index keeps it cheap as suspensions accumulate.
CREATE INDEX execution_suspensions_active_lookup_idx
    ON execution_suspensions (reason, resume_key)
    WHERE resolved_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS execution_suspensions;
DROP TABLE IF EXISTS execution_turns;
