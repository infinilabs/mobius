CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
-- EMPLOYEES
-- ============================================================

CREATE TABLE IF NOT EXISTS employees (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    role            TEXT NOT NULL DEFAULT 'Custom'
                    CHECK (role IN ('CEO','PM','Engineer','QA','Designer','Custom')),
    backstory       TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    adapter_type    TEXT NOT NULL DEFAULT 'internal_llm',
    adapter_config  JSONB NOT NULL DEFAULT '{}',
    monthly_budget  INT CHECK (monthly_budget IS NULL OR monthly_budget >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS employee_models (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    model_id    TEXT NOT NULL,
    purpose     TEXT NOT NULL DEFAULT 'primary_llm'
                CHECK (purpose IN ('primary_llm','image_gen','video_gen','code_gen','analysis')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, purpose)
);

CREATE TABLE IF NOT EXISTS employee_skills (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    skill       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    UNIQUE (employee_id, skill)
);

CREATE TABLE IF NOT EXISTS employee_tags (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    UNIQUE (employee_id, tag)
);

CREATE TABLE IF NOT EXISTS employee_reporting (
    employee_id UUID PRIMARY KEY REFERENCES employees(id) ON DELETE CASCADE,
    manager_id  UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    CHECK (employee_id != manager_id)
);

CREATE INDEX IF NOT EXISTS idx_employee_models_employee ON employee_models(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_skills_employee ON employee_skills(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_tags_employee ON employee_tags(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_tags_tag ON employee_tags(tag);
CREATE INDEX IF NOT EXISTS idx_employee_reporting_manager ON employee_reporting(manager_id);

-- ============================================================
-- PROJECTS
-- ============================================================

CREATE TABLE IF NOT EXISTS projects (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            TEXT NOT NULL UNIQUE,
    description     TEXT NOT NULL DEFAULT '',
    owner_id        UUID REFERENCES employees(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'paused')),
    source_path     TEXT,
    tags            TEXT[] NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id);
CREATE INDEX IF NOT EXISTS idx_projects_tags ON projects USING GIN(tags);

-- ============================================================
-- GOALS
-- ============================================================

CREATE TABLE IF NOT EXISTS goals (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id   UUID REFERENCES goals(id) ON DELETE SET NULL,
    project_id  UUID REFERENCES projects(id) ON DELETE SET NULL,
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active','achieved','abandoned')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_goals_parent ON goals(parent_id);
CREATE INDEX IF NOT EXISTS idx_goals_project ON goals(project_id);
CREATE INDEX IF NOT EXISTS idx_goals_status ON goals(status);

-- ============================================================
-- TASKS
-- ============================================================

CREATE TABLE IF NOT EXISTS tasks (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'todo'
                    CHECK (status IN ('todo','ready','in_progress','needs_review','done','blocked','scheduled')),
    priority        TEXT NOT NULL DEFAULT 'medium'
                    CHECK (priority IN ('low','medium','high','urgent')),
    assignee_id     UUID REFERENCES employees(id) ON DELETE SET NULL,
    creator_id      UUID REFERENCES employees(id) ON DELETE SET NULL,
    goal_id         UUID REFERENCES goals(id) ON DELETE SET NULL,
    project_id      UUID REFERENCES projects(id) ON DELETE SET NULL,
    result          TEXT NOT NULL DEFAULT '',
    failure_count   INT NOT NULL DEFAULT 0,
    conversation_id TEXT,
    retry_after     TIMESTAMPTZ,
    is_scheduled    BOOLEAN NOT NULL DEFAULT FALSE,
    cron_expr       TEXT,
    next_run_at     TIMESTAMPTZ,
    repeat_times    INT,
    parent_task_id  UUID REFERENCES tasks(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id     UUID REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on  UUID REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on),
    CONSTRAINT chk_no_self_dep CHECK (task_id != depends_on)
);

CREATE TABLE IF NOT EXISTS task_comments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id   UUID REFERENCES employees(id) ON DELETE SET NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee_id);
CREATE INDEX IF NOT EXISTS idx_tasks_goal ON tasks(goal_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_conversation ON tasks(conversation_id);
CREATE INDEX IF NOT EXISTS idx_tasks_retry_after ON tasks(status, retry_after) WHERE status = 'ready';
CREATE INDEX IF NOT EXISTS idx_tasks_schedule_trigger ON tasks(next_run_at) WHERE is_scheduled = TRUE AND next_run_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id) WHERE parent_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_dependencies(depends_on);
CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments(task_id, created_at);

-- ============================================================
-- TASK INTERACTIONS (A2A blocking questions / suggestions)
-- ============================================================

CREATE TABLE IF NOT EXISTS task_interactions (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id             UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    creator_employee_id UUID NOT NULL REFERENCES employees(id),
    kind                TEXT NOT NULL
                        CHECK (kind IN ('ask_user', 'suggest_tasks', 'request_approval')),
    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'resolved', 'dismissed')),
    payload             JSONB NOT NULL DEFAULT '{}',
    response            JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ,
    resolved_by         UUID REFERENCES employees(id)
);

CREATE INDEX IF NOT EXISTS idx_task_interactions_task ON task_interactions(task_id);
CREATE INDEX IF NOT EXISTS idx_task_interactions_pending ON task_interactions(status) WHERE status = 'pending';

-- ============================================================
-- HEARTBEAT RUNS (adapter execution tracking)
-- ============================================================

CREATE TABLE IF NOT EXISTS heartbeat_runs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id         UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    adapter_type    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','completed','failed','cancelled')),
    output_text     TEXT NOT NULL DEFAULT '',
    error_message   TEXT,
    token_usage     JSONB,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_heartbeat_runs_task ON heartbeat_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_heartbeat_runs_agent ON heartbeat_runs(agent_id);
CREATE INDEX IF NOT EXISTS idx_heartbeat_runs_active ON heartbeat_runs(status) WHERE status = 'active';

-- ============================================================
-- DISPATCH EVENTS (reactive LISTEN/NOTIFY bus)
-- ============================================================

CREATE TABLE IF NOT EXISTS dispatch_events (
    id          BIGSERIAL PRIMARY KEY,
    channel     TEXT NOT NULL,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dispatch_events_created ON dispatch_events(created_at);

-- ============================================================
-- SKILL ASSIGNMENTS
-- ============================================================

CREATE TABLE IF NOT EXISTS skill_assignments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    skill_id    TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, skill_id)
);

CREATE INDEX IF NOT EXISTS idx_skill_assignments_employee ON skill_assignments(employee_id);
CREATE INDEX IF NOT EXISTS idx_skill_assignments_skill ON skill_assignments(skill_id);

-- ============================================================
-- CONVERSATIONS
-- ============================================================

CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL DEFAULT '',
    project_id      UUID REFERENCES projects(id) ON DELETE SET NULL,
    created_at      BIGINT NOT NULL,
    updated_at      BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversations_project ON conversations(project_id);

-- tasks.conversation_id references conversations(id). Added here (not inline on
-- the tasks table) because conversations is defined after tasks; the DO block
-- keeps the whole file re-runnable (ADD CONSTRAINT has no IF NOT EXISTS).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_conversation'
    ) THEN
        ALTER TABLE tasks
            ADD CONSTRAINT fk_tasks_conversation
            FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL;
    END IF;
END $$;

-- ============================================================
-- PG FUNCTIONS & TRIGGERS (reactive dispatch)
-- ============================================================

CREATE OR REPLACE FUNCTION notify_dispatch_event()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('mobius_dispatch', json_build_object(
        'id', NEW.id,
        'channel', NEW.channel
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_dispatch_event ON dispatch_events;
CREATE TRIGGER trg_dispatch_event
AFTER INSERT ON dispatch_events
FOR EACH ROW EXECUTE FUNCTION notify_dispatch_event();

CREATE OR REPLACE FUNCTION on_task_status_change()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'ready' AND NEW.assignee_id IS NOT NULL
       AND (OLD.status IS DISTINCT FROM NEW.status) THEN
        INSERT INTO dispatch_events (channel, payload)
        VALUES ('task_ready', json_build_object(
            'task_id', NEW.id,
            'assignee_id', NEW.assignee_id
        ));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_task_status_change ON tasks;
CREATE TRIGGER trg_task_status_change
AFTER UPDATE OF status ON tasks
FOR EACH ROW EXECUTE FUNCTION on_task_status_change();

CREATE OR REPLACE FUNCTION on_interaction_resolved()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'resolved' AND OLD.status = 'pending' THEN
        INSERT INTO dispatch_events (channel, payload)
        VALUES ('interaction_resolved', json_build_object(
            'interaction_id', NEW.id,
            'task_id', NEW.task_id,
            'creator_employee_id', NEW.creator_employee_id
        ));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_interaction_resolved ON task_interactions;
CREATE TRIGGER trg_interaction_resolved
AFTER UPDATE OF status ON task_interactions
FOR EACH ROW EXECUTE FUNCTION on_interaction_resolved();
