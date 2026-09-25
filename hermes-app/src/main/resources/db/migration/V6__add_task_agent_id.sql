ALTER TABLE tasks ADD COLUMN agent_id VARCHAR(100);
UPDATE tasks SET agent_id = 'general' WHERE agent_id IS NULL;
ALTER TABLE tasks ALTER COLUMN agent_id SET NOT NULL;
