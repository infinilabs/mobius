package domain

import "time"

type Employee struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Role          string          `json:"role"`
	Backstory     string          `json:"backstory"`
	AvatarURL     string          `json:"avatar_url"`
	AdapterType   string          `json:"adapter_type"`
	AdapterConfig map[string]any  `json:"adapter_config"`
	MonthlyBudget *int            `json:"monthly_budget,omitempty"`
	Models        []EmployeeModel `json:"models"`
	Skills        []EmployeeSkill `json:"skills"`
	Tags          []string        `json:"tags"`
	ManagerID     *string         `json:"manager_id"`
	Reports       []EmployeeBrief `json:"reports"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type EmployeeBrief struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Role  string `json:"role"`
}

type EmployeeModel struct {
	ModelID string `json:"model_id"`
	Purpose string `json:"purpose"`
}

type EmployeeSkill struct {
	Skill       string `json:"skill"`
	Description string `json:"description"`
}

// HasTag reports whether tags contains target.
func HasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}
