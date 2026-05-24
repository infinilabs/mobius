CREATE INDEX IF NOT EXISTS idx_tasks_retry_after ON tasks(status, retry_after)
    WHERE status = 'ready';
