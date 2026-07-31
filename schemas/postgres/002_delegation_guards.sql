-- Phase 1 remediation (plan 1.1 / 1.6): bound autonomous loops.
--   delegation_depth: each delegated task carries its parent's depth + 1 so
--   delegation chains (including A->B->A ping-pong) terminate at a max depth.
--   rejection_count: incremented on every needs_review -> ready rejection so a
--   chronically-rejected task can be parked for human review instead of
--   re-arming the auto-reviewer forever.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS delegation_depth INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS rejection_count INT NOT NULL DEFAULT 0;
