CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

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

CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL DEFAULT '',
    project_id      UUID REFERENCES projects(id) ON DELETE SET NULL,
    created_at      BIGINT NOT NULL,
    updated_at      BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversations_project ON conversations(project_id);

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
