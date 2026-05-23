ALTER TABLE tasks ADD COLUMN IF NOT EXISTS conversation_id TEXT;
CREATE INDEX IF NOT EXISTS idx_tasks_conversation ON tasks(conversation_id);
