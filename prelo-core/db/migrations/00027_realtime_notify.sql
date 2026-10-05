-- +goose Up
-- PR-4 (contratos G4): real-time hints for the web and the app. Triggers announce *that* something
-- changed (kind, id, project, status) on the "prelo_events" channel — never content. Screens
-- re-fetch through the normal, authorized API; the stream only says when.

-- +goose StatementBegin
CREATE FUNCTION prelo_app.notify_event(kind text, id uuid, project_id uuid, status text, extra jsonb DEFAULT '{}'::jsonb)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('prelo_events', (jsonb_build_object(
        'kind', kind, 'id', id, 'projectId', project_id, 'status', status, 'at', now()) || extra)::text);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION prelo_app.tasks_notify() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM prelo_app.notify_event('task', NEW.id, NEW.project_id, NEW.status,
            jsonb_build_object('parentId', NEW.parent_task_id));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tasks_notify AFTER INSERT OR UPDATE OF status ON prelo_app.tasks
    FOR EACH ROW EXECUTE FUNCTION prelo_app.tasks_notify();

-- +goose StatementBegin
CREATE FUNCTION prelo_app.turns_notify() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t_id uuid; p_id uuid;
BEGIN
    SELECT e.task_id, t.project_id INTO t_id, p_id
      FROM prelo_app.task_executions e JOIN prelo_app.tasks t ON t.id = e.task_id WHERE e.id = NEW.execution_id;
    PERFORM prelo_app.notify_event('execution', NEW.execution_id, p_id, NEW.kind, jsonb_build_object('taskId', t_id));
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER turns_notify AFTER INSERT OR UPDATE ON prelo_app.execution_turns
    FOR EACH ROW EXECUTE FUNCTION prelo_app.turns_notify();

-- +goose StatementBegin
CREATE FUNCTION prelo_app.approvals_notify() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p_id uuid;
BEGIN
    IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        IF NEW.action_request_id IS NOT NULL THEN
            SELECT project_id INTO p_id FROM prelo_app.action_requests WHERE id = NEW.action_request_id;
        ELSE
            SELECT t.project_id INTO p_id FROM prelo_app.tool_calls tc JOIN prelo_app.tasks t ON t.id = tc.task_id WHERE tc.id = NEW.tool_call_id;
        END IF;
        PERFORM prelo_app.notify_event('approval', NEW.id, p_id, NEW.status,
            jsonb_build_object('actionRequestId', NEW.action_request_id));
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER approvals_notify AFTER INSERT OR UPDATE OF status ON prelo_app.approval_requests
    FOR EACH ROW EXECUTE FUNCTION prelo_app.approvals_notify();

-- +goose StatementBegin
CREATE FUNCTION prelo_app.actions_notify() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.result_status IS DISTINCT FROM OLD.result_status THEN
        PERFORM prelo_app.notify_event('action', NEW.id, NEW.project_id, NEW.result_status, '{}'::jsonb);
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER actions_notify AFTER UPDATE OF result_status ON prelo_app.action_requests
    FOR EACH ROW EXECUTE FUNCTION prelo_app.actions_notify();

-- +goose Down
DROP TRIGGER IF EXISTS actions_notify ON prelo_app.action_requests;
DROP TRIGGER IF EXISTS approvals_notify ON prelo_app.approval_requests;
DROP TRIGGER IF EXISTS turns_notify ON prelo_app.execution_turns;
DROP TRIGGER IF EXISTS tasks_notify ON prelo_app.tasks;
DROP FUNCTION IF EXISTS prelo_app.actions_notify();
DROP FUNCTION IF EXISTS prelo_app.approvals_notify();
DROP FUNCTION IF EXISTS prelo_app.turns_notify();
DROP FUNCTION IF EXISTS prelo_app.tasks_notify();
DROP FUNCTION IF EXISTS prelo_app.notify_event(text, uuid, uuid, text, jsonb);
