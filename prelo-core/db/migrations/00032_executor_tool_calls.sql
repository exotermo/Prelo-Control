-- +goose Up
-- Correlate model tool calls with their exact executor request and the existing approval row.
ALTER TABLE prelo_app.executor_requests
    ADD COLUMN tool_call_id UUID REFERENCES prelo_app.tool_calls(id),
    ADD COLUMN approval_id UUID REFERENCES prelo_app.approval_requests(id);
CREATE UNIQUE INDEX executor_requests_tool_call_uk ON prelo_app.executor_requests(tool_call_id)
    WHERE tool_call_id IS NOT NULL;
CREATE UNIQUE INDEX executor_requests_approval_uk ON prelo_app.executor_requests(approval_id)
    WHERE approval_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS prelo_app.executor_requests_approval_uk;
DROP INDEX IF EXISTS prelo_app.executor_requests_tool_call_uk;
ALTER TABLE prelo_app.executor_requests DROP COLUMN IF EXISTS approval_id, DROP COLUMN IF EXISTS tool_call_id;
