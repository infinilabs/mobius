ALTER TABLE tasks ADD COLUMN IF NOT EXISTS is_scheduled    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS cron_expr       TEXT;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS next_run_at     TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS repeat_times    INT;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS parent_task_id  UUID REFERENCES tasks(id) ON DELETE SET NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('todo','ready','in_progress','needs_review','done','blocked','scheduled'));

CREATE INDEX IF NOT EXISTS idx_tasks_schedule_trigger
    ON tasks(next_run_at)
    WHERE is_scheduled = TRUE AND next_run_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_parent
    ON tasks(parent_task_id)
    WHERE parent_task_id IS NOT NULL;
