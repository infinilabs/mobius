CREATE TABLE IF NOT EXISTS skill_assignments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    skill_id    TEXT NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, skill_id)
);

CREATE INDEX IF NOT EXISTS idx_skill_assignments_employee ON skill_assignments(employee_id);
CREATE INDEX IF NOT EXISTS idx_skill_assignments_skill ON skill_assignments(skill_id);
