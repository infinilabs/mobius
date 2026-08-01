package domain

import (
	"encoding/json"
	"time"
)

type TaskInteraction struct {
	ID                string          `json:"id"`
	TaskID            string          `json:"task_id"`
	CreatorEmployeeID string          `json:"creator_employee_id"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	Payload           json.RawMessage `json:"payload"`
	Response          json.RawMessage `json:"response,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	ResolvedAt        *time.Time      `json:"resolved_at,omitempty"`
	ResolvedBy        *string         `json:"resolved_by,omitempty"`
}
