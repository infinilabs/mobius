package domain

import "time"

type Task struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Status       string         `json:"status"`
	Priority     string         `json:"priority"`
	Assignee     *EmployeeBrief `json:"assignee"`
	Creator      *EmployeeBrief `json:"creator"`
	Result       string         `json:"result"`
	FailureCount int            `json:"failure_count"`
	// DelegationDepth counts hops from the root task (plan 1.1). Populated by
	// GetTask; list queries leave it zero since only delegation paths need it.
	DelegationDepth int        `json:"delegation_depth"`
	Dependencies    []string   `json:"dependencies"`
	IsScheduled     bool       `json:"is_scheduled"`
	CronExpr        string     `json:"cron_expr,omitempty"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	RepeatTimes     *int       `json:"repeat_times,omitempty"`
	ParentTaskID    *string    `json:"parent_task_id,omitempty"`
	ProjectID       *string    `json:"project_id,omitempty"`
	ProjectName     string     `json:"project_name,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type TaskComment struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"task_id"`
	Author    *EmployeeBrief `json:"author"`
	Content   string         `json:"content"`
	CreatedAt time.Time      `json:"created_at"`
}
