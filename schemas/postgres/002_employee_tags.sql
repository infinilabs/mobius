CREATE TABLE IF NOT EXISTS employee_tags (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    UNIQUE (employee_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_employee_tags_employee ON employee_tags(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_tags_tag ON employee_tags(tag);
