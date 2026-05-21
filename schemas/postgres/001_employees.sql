CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS employees (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT 'Custom'
                CHECK (role IN ('CEO','PM','Engineer','QA','Designer','Custom')),
    backstory   TEXT NOT NULL DEFAULT '',
    avatar_url  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

CREATE TABLE IF NOT EXISTS employee_reporting (
    employee_id UUID PRIMARY KEY REFERENCES employees(id) ON DELETE CASCADE,
    manager_id  UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    CHECK (employee_id != manager_id)
);

CREATE INDEX IF NOT EXISTS idx_employee_models_employee ON employee_models(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_skills_employee ON employee_skills(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_reporting_manager ON employee_reporting(manager_id);
