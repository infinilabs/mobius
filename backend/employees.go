package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type Employee struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Title     string          `json:"title"`
	Role      string          `json:"role"`
	Backstory string          `json:"backstory"`
	AvatarURL string          `json:"avatar_url"`
	Models    []EmployeeModel `json:"models"`
	Skills    []EmployeeSkill `json:"skills"`
	Tags      []string        `json:"tags"`
	ManagerID *string         `json:"manager_id"`
	Reports   []EmployeeBrief `json:"reports"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
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

// PG operations

func (pg *PGClient) ListEmployees(ctx context.Context) ([]Employee, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT e.id, e.name, e.title, e.role, e.backstory, e.avatar_url,
		       e.created_at, e.updated_at, r.manager_id
		FROM employees e
		LEFT JOIN employee_reporting r ON r.employee_id = e.id
		ORDER BY e.created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("list employees: %w", err)
	}
	defer rows.Close()

	var employees []Employee
	ids := make([]string, 0)
	for rows.Next() {
		var emp Employee
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.Title, &emp.Role, &emp.Backstory,
			&emp.AvatarURL, &emp.CreatedAt, &emp.UpdatedAt, &emp.ManagerID); err != nil {
			return nil, fmt.Errorf("scan employee: %w", err)
		}
		emp.Models = []EmployeeModel{}
		emp.Skills = []EmployeeSkill{}
		emp.Tags = []string{}
		emp.Reports = []EmployeeBrief{}
		employees = append(employees, emp)
		ids = append(ids, emp.ID)
	}

	if len(employees) == 0 {
		return []Employee{}, nil
	}

	modelsMap, err := pg.batchLoadModels(ctx, ids)
	if err != nil {
		return nil, err
	}
	skillsMap, err := pg.batchLoadSkills(ctx, ids)
	if err != nil {
		return nil, err
	}
	tagsMap, err := pg.batchLoadTags(ctx, ids)
	if err != nil {
		return nil, err
	}

	empIndex := make(map[string]int, len(employees))
	for i := range employees {
		empIndex[employees[i].ID] = i
		if m, ok := modelsMap[employees[i].ID]; ok {
			employees[i].Models = m
		}
		if s, ok := skillsMap[employees[i].ID]; ok {
			employees[i].Skills = s
		}
		if t, ok := tagsMap[employees[i].ID]; ok {
			employees[i].Tags = t
		}
	}

	for i := range employees {
		if employees[i].ManagerID != nil {
			if mi, ok := empIndex[*employees[i].ManagerID]; ok {
				employees[mi].Reports = append(employees[mi].Reports, EmployeeBrief{
					ID: employees[i].ID, Name: employees[i].Name,
					Title: employees[i].Title, Role: employees[i].Role,
				})
			}
		}
	}

	return employees, nil
}

func (pg *PGClient) batchLoadModels(ctx context.Context, ids []string) (map[string][]EmployeeModel, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT employee_id, model_id, purpose FROM employee_models WHERE employee_id = ANY($1)",
		ids)
	if err != nil {
		return nil, fmt.Errorf("batch load models: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]EmployeeModel)
	for rows.Next() {
		var empID string
		var m EmployeeModel
		if err := rows.Scan(&empID, &m.ModelID, &m.Purpose); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result[empID] = append(result[empID], m)
	}
	return result, nil
}

func (pg *PGClient) batchLoadTags(ctx context.Context, ids []string) (map[string][]string, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT employee_id, tag FROM employee_tags WHERE employee_id = ANY($1) ORDER BY tag",
		ids)
	if err != nil {
		return nil, fmt.Errorf("batch load tags: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var empID, tag string
		if err := rows.Scan(&empID, &tag); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		result[empID] = append(result[empID], tag)
	}
	return result, nil
}

func (pg *PGClient) batchLoadSkills(ctx context.Context, ids []string) (map[string][]EmployeeSkill, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT employee_id, skill, description FROM employee_skills WHERE employee_id = ANY($1)",
		ids)
	if err != nil {
		return nil, fmt.Errorf("batch load skills: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]EmployeeSkill)
	for rows.Next() {
		var empID string
		var s EmployeeSkill
		if err := rows.Scan(&empID, &s.Skill, &s.Description); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		result[empID] = append(result[empID], s)
	}
	return result, nil
}

func (pg *PGClient) GetEmployee(ctx context.Context, id string) (*Employee, error) {
	var emp Employee
	err := pg.pool.QueryRow(ctx, `
		SELECT e.id, e.name, e.title, e.role, e.backstory, e.avatar_url,
		       e.created_at, e.updated_at, r.manager_id
		FROM employees e
		LEFT JOIN employee_reporting r ON r.employee_id = e.id
		WHERE e.id = $1
	`, id).Scan(&emp.ID, &emp.Name, &emp.Title, &emp.Role, &emp.Backstory,
		&emp.AvatarURL, &emp.CreatedAt, &emp.UpdatedAt, &emp.ManagerID)
	if err != nil {
		return nil, fmt.Errorf("get employee: %w", err)
	}

	emp.Models = []EmployeeModel{}
	emp.Skills = []EmployeeSkill{}
	emp.Tags = []string{}
	emp.Reports = []EmployeeBrief{}

	modelsMap, _ := pg.batchLoadModels(ctx, []string{id})
	if m, ok := modelsMap[id]; ok {
		emp.Models = m
	}

	skillsMap, _ := pg.batchLoadSkills(ctx, []string{id})
	if s, ok := skillsMap[id]; ok {
		emp.Skills = s
	}

	tagsMap, _ := pg.batchLoadTags(ctx, []string{id})
	if t, ok := tagsMap[id]; ok {
		emp.Tags = t
	}

	reportRows, err := pg.pool.Query(ctx, `
		SELECT e.id, e.name, e.title, e.role
		FROM employees e
		JOIN employee_reporting r ON r.employee_id = e.id
		WHERE r.manager_id = $1
	`, id)
	if err == nil {
		defer reportRows.Close()
		for reportRows.Next() {
			var b EmployeeBrief
			if err := reportRows.Scan(&b.ID, &b.Name, &b.Title, &b.Role); err == nil {
				emp.Reports = append(emp.Reports, b)
			}
		}
	}

	return &emp, nil
}

func (pg *PGClient) CreateEmployee(ctx context.Context, emp *Employee) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO employees (name, title, role, backstory, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, emp.Name, emp.Title, emp.Role, emp.Backstory, emp.AvatarURL).Scan(
		&emp.ID, &emp.CreatedAt, &emp.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert employee: %w", err)
	}

	if err := pg.insertRelated(ctx, tx, emp.ID, emp.Models, emp.Skills, emp.Tags); err != nil {
		return err
	}

	if emp.ManagerID != nil && *emp.ManagerID != "" {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)",
			emp.ID, *emp.ManagerID); err != nil {
			return fmt.Errorf("insert reporting: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (pg *PGClient) insertRelated(ctx context.Context, tx pgx.Tx, empID string, models []EmployeeModel, skills []EmployeeSkill, tags []string) error {
	for _, m := range models {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_models (employee_id, model_id, purpose) VALUES ($1, $2, $3)",
			empID, m.ModelID, m.Purpose); err != nil {
			return fmt.Errorf("insert model: %w", err)
		}
	}
	for _, s := range skills {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_skills (employee_id, skill, description) VALUES ($1, $2, $3)",
			empID, s.Skill, s.Description); err != nil {
			return fmt.Errorf("insert skill: %w", err)
		}
	}
	for _, t := range tags {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_tags (employee_id, tag) VALUES ($1, $2)",
			empID, t); err != nil {
			return fmt.Errorf("insert tag: %w", err)
		}
	}
	return nil
}

func (pg *PGClient) UpdateEmployee(ctx context.Context, id string, emp *Employee) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE employees
		SET name=$1, title=$2, role=$3, backstory=$4, avatar_url=$5, updated_at=NOW()
		WHERE id=$6
	`, emp.Name, emp.Title, emp.Role, emp.Backstory, emp.AvatarURL, id)
	if err != nil {
		return fmt.Errorf("update employee: %w", err)
	}

	tx.Exec(ctx, "DELETE FROM employee_models WHERE employee_id=$1", id)
	tx.Exec(ctx, "DELETE FROM employee_skills WHERE employee_id=$1", id)
	tx.Exec(ctx, "DELETE FROM employee_tags WHERE employee_id=$1", id)

	if err := pg.insertRelated(ctx, tx, id, emp.Models, emp.Skills, emp.Tags); err != nil {
		return err
	}

	tx.Exec(ctx, "DELETE FROM employee_reporting WHERE employee_id=$1", id)
	if emp.ManagerID != nil && *emp.ManagerID != "" {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)",
			id, *emp.ManagerID); err != nil {
			return fmt.Errorf("update reporting: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (pg *PGClient) DeleteEmployee(ctx context.Context, id string) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var managerID *string
	pg.pool.QueryRow(ctx, "SELECT manager_id FROM employee_reporting WHERE employee_id=$1", id).Scan(&managerID)

	if managerID != nil {
		tx.Exec(ctx, `
			UPDATE employee_reporting SET manager_id=$1
			WHERE manager_id=$2
		`, *managerID, id)
	} else {
		tx.Exec(ctx, "DELETE FROM employee_reporting WHERE manager_id=$1", id)
	}

	_, err = tx.Exec(ctx, "DELETE FROM employees WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("delete employee: %w", err)
	}

	return tx.Commit(ctx)
}

func (pg *PGClient) SetManager(ctx context.Context, employeeID, managerID string) error {
	if employeeID == managerID {
		return fmt.Errorf("employee cannot be their own manager")
	}

	current := managerID
	for i := 0; i < 50; i++ {
		var parentID *string
		err := pg.pool.QueryRow(ctx,
			"SELECT manager_id FROM employee_reporting WHERE employee_id=$1",
			current).Scan(&parentID)
		if err != nil {
			break
		}
		if *parentID == employeeID {
			return fmt.Errorf("circular reporting chain detected")
		}
		current = *parentID
	}

	_, err := pg.pool.Exec(ctx, `
		INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)
		ON CONFLICT (employee_id) DO UPDATE SET manager_id = $2
	`, employeeID, managerID)
	if err != nil {
		return fmt.Errorf("set manager: %w", err)
	}
	return nil
}

func (pg *PGClient) CountEmployees(ctx context.Context) (int, error) {
	var count int
	err := pg.pool.QueryRow(ctx, "SELECT COUNT(*) FROM employees").Scan(&count)
	return count, err
}

func (pg *PGClient) backfillDefaultTags(ctx context.Context) {
	defaults := map[string]string{
		"Elong": "executive",
		"Steve": "manager",
		"Linas": "manager",
		"Allen": "manager",
	}
	for name, tag := range defaults {
		var id string
		err := pg.pool.QueryRow(ctx, "SELECT id FROM employees WHERE name=$1", name).Scan(&id)
		if err != nil {
			continue
		}
		pg.pool.Exec(ctx,
			"INSERT INTO employee_tags (employee_id, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			id, tag)
	}
}

func (pg *PGClient) SeedDefaultEmployees(ctx context.Context) error {
	count, err := pg.CountEmployees(ctx)
	if err != nil {
		return fmt.Errorf("count employees: %w", err)
	}
	if count > 0 {
		pg.backfillDefaultTags(ctx)
		return nil
	}

	type seed struct {
		Name      string
		Title     string
		Role      string
		Backstory string
		Skills    []EmployeeSkill
		Tags      []string
	}

	defaults := []seed{
		{
			Name:  "Elong",
			Title: "Chief Executive Officer",
			Role:  "CEO",
			Backstory: "A visionary leader who reasons from first principles. " +
				"Despises bureaucracy and moves at breakneck speed. " +
				"Understands engineering and business deeply. " +
				"Distributes assignments and audits final outcomes.",
			Skills: []EmployeeSkill{
				{"Strategic Vision", "Decompose ideas into actionable project phases"},
				{"First Principles Thinking", "Challenge assumptions and find optimal solutions"},
				{"Team Leadership", "Coordinate cross-functional teams effectively"},
				{"Executive Review", "Synthesize outputs into delivery packages"},
			},
			Tags: []string{"executive"},
		},
		{
			Name:  "Steve",
			Title: "Product Manager",
			Role:  "PM",
			Backstory: "Obsessive eye for aesthetics and simplicity. " +
				"Believes code means nothing if the user experience isn't beautiful. " +
				"Turns technical notes into clean user flows and stunning interface specs.",
			Skills: []EmployeeSkill{
				{"Product Strategy", "Transform objectives into intuitive product experiences"},
				{"User Experience", "Design minimalist and human-centric interfaces"},
				{"Roadmap Planning", "Prioritize features by impact and feasibility"},
				{"Specification Writing", "Create detailed product blueprints"},
			},
			Tags: []string{"manager"},
		},
		{
			Name:  "Linas",
			Title: "Senior Engineer",
			Role:  "Engineer",
			Backstory: "Despises bloated software. Demands extreme runtime efficiency, " +
				"modular architecture, and clean self-documenting code. " +
				"Masters system infrastructure, databases, and advanced rendering.",
			Skills: []EmployeeSkill{
				{"System Design", "Architect high-performance backends and frontends"},
				{"Full-Stack Development", "Write production-ready modular code"},
				{"Code Review", "Ensure code quality and performance standards"},
				{"Performance Optimization", "Eliminate bottlenecks and reduce latency"},
			},
			Tags: []string{"manager"},
		},
		{
			Name:  "Allen",
			Title: "QA Lead",
			Role:  "QA",
			Backstory: "Meticulous and calculated. Breaks what engineers build " +
				"so it becomes bulletproof. Checks backend logic, API edge cases, " +
				"and frontend responsiveness. Tolerates zero bugs.",
			Skills: []EmployeeSkill{
				{"Test Strategy", "Build comprehensive test suites covering unit/integration/e2e"},
				{"Bug Analysis", "Trace edge cases and identify runtime issues"},
				{"Quality Metrics", "Track and report on code health and coverage"},
				{"Automated Testing", "Design and maintain automated test frameworks"},
			},
			Tags: []string{"manager"},
		},
	}

	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed tx: %w", err)
	}
	defer tx.Rollback(ctx)

	employeeIDs := make([]string, len(defaults))

	for i, d := range defaults {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO employees (name, title, role, backstory)
			VALUES ($1, $2, $3, $4) RETURNING id
		`, d.Name, d.Title, d.Role, d.Backstory).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed employee %s: %w", d.Name, err)
		}
		employeeIDs[i] = id

		for _, s := range d.Skills {
			if _, err := tx.Exec(ctx,
				"INSERT INTO employee_skills (employee_id, skill, description) VALUES ($1, $2, $3)",
				id, s.Skill, s.Description); err != nil {
				return fmt.Errorf("seed skill for %s: %w", d.Name, err)
			}
		}
		for _, t := range d.Tags {
			if _, err := tx.Exec(ctx,
				"INSERT INTO employee_tags (employee_id, tag) VALUES ($1, $2)",
				id, t); err != nil {
				return fmt.Errorf("seed tag for %s: %w", d.Name, err)
			}
		}
	}

	for i := 1; i < len(employeeIDs); i++ {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)",
			employeeIDs[i], employeeIDs[0]); err != nil {
			return fmt.Errorf("seed reporting for index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}

	slog.Info("default employees seeded", "count", len(defaults))
	return nil
}

// HTTP handlers

func (h *APIHandler) ListEmployees(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	employees, err := h.pgClient.ListEmployees(r.Context())
	if err != nil {
		writeError(w, "failed to list employees: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, employees)
}

func (h *APIHandler) GetEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	emp, err := h.pgClient.GetEmployee(r.Context(), id)
	if err != nil {
		writeError(w, "employee not found", http.StatusNotFound)
		return
	}
	writeJSON(w, emp)
}

func (h *APIHandler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	var emp Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if emp.Name == "" {
		writeError(w, "name is required", http.StatusBadRequest)
		return
	}
	if emp.Role == "" {
		emp.Role = "Custom"
	}
	if emp.Models == nil {
		emp.Models = []EmployeeModel{}
	}
	if emp.Skills == nil {
		emp.Skills = []EmployeeSkill{}
	}
	if emp.Tags == nil {
		emp.Tags = []string{}
	}

	if err := h.pgClient.CreateEmployee(r.Context(), &emp); err != nil {
		writeError(w, "failed to create employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("employee created", "id", emp.ID, "name", emp.Name)
	writeJSON(w, emp)
}

func (h *APIHandler) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var emp Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if emp.Models == nil {
		emp.Models = []EmployeeModel{}
	}
	if emp.Skills == nil {
		emp.Skills = []EmployeeSkill{}
	}
	if emp.Tags == nil {
		emp.Tags = []string{}
	}

	if err := h.pgClient.UpdateEmployee(r.Context(), id, &emp); err != nil {
		writeError(w, "failed to update employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updated, _ := h.pgClient.GetEmployee(r.Context(), id)
	if updated == nil {
		writeJSON(w, emp)
		return
	}
	slog.Info("employee updated", "id", id)
	writeJSON(w, updated)
}

func (h *APIHandler) DeleteEmployee(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	if err := h.pgClient.DeleteEmployee(r.Context(), id); err != nil {
		writeError(w, "failed to delete employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("employee deleted", "id", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *APIHandler) SetEmployeeManager(w http.ResponseWriter, r *http.Request) {
	if h.pgClient == nil {
		writeError(w, "PostgreSQL not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	var body struct {
		ManagerID string `json:"manager_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.ManagerID == "" {
		_, err := h.pgClient.pool.Exec(r.Context(), "DELETE FROM employee_reporting WHERE employee_id=$1", id)
		if err != nil {
			writeError(w, "failed to remove manager: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.pgClient.SetManager(r.Context(), id, body.ManagerID); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	slog.Info("employee manager updated", "id", id, "manager_id", body.ManagerID)
	writeJSON(w, map[string]string{"status": "ok"})
}
