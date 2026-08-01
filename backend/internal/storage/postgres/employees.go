package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/seed"

	"github.com/jackc/pgx/v5"
)

// PG operations

// rollbackTx is a defer-friendly rollback. pgx returns ErrTxClosed after a
// successful Commit (the normal path); anything else is a real failure.
func rollbackTx(ctx context.Context, tx pgx.Tx, op string) {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("tx rollback failed", "op", op, "error", err)
	}
}

func (pg *Client) ListEmployees(ctx context.Context) ([]domain.Employee, error) {
	rows, err := pg.pool.Query(ctx, `
		SELECT e.id, e.name, e.title, e.role, e.backstory, e.avatar_url,
		       e.adapter_type, e.adapter_config, e.monthly_budget,
		       e.created_at, e.updated_at, r.manager_id
		FROM employees e
		LEFT JOIN employee_reporting r ON r.employee_id = e.id
		ORDER BY e.created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("list employees: %w", err)
	}
	defer rows.Close()

	var employees []domain.Employee
	ids := make([]string, 0)
	for rows.Next() {
		var emp domain.Employee
		var adapterConfig []byte
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.Title, &emp.Role, &emp.Backstory,
			&emp.AvatarURL, &emp.AdapterType, &adapterConfig, &emp.MonthlyBudget,
			&emp.CreatedAt, &emp.UpdatedAt, &emp.ManagerID); err != nil {
			return nil, fmt.Errorf("scan employee: %w", err)
		}
		emp.AdapterConfig = make(map[string]any)
		if len(adapterConfig) > 0 {
			if err := json.Unmarshal(adapterConfig, &emp.AdapterConfig); err != nil {
				return nil, fmt.Errorf("decode adapter config for employee %s: %w", emp.ID, err)
			}
		}
		emp.Models = []domain.EmployeeModel{}
		emp.Skills = []domain.EmployeeSkill{}
		emp.Tags = []string{}
		emp.Reports = []domain.EmployeeBrief{}
		employees = append(employees, emp)
		ids = append(ids, emp.ID)
	}

	if len(employees) == 0 {
		return []domain.Employee{}, nil
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
				employees[mi].Reports = append(employees[mi].Reports, domain.EmployeeBrief{
					ID: employees[i].ID, Name: employees[i].Name,
					Title: employees[i].Title, Role: employees[i].Role,
				})
			}
		}
	}

	return employees, nil
}

func (pg *Client) batchLoadModels(ctx context.Context, ids []string) (map[string][]domain.EmployeeModel, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT employee_id, model_id, purpose FROM employee_models WHERE employee_id = ANY($1)",
		ids)
	if err != nil {
		return nil, fmt.Errorf("batch load models: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]domain.EmployeeModel)
	for rows.Next() {
		var empID string
		var m domain.EmployeeModel
		if err := rows.Scan(&empID, &m.ModelID, &m.Purpose); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result[empID] = append(result[empID], m)
	}
	return result, nil
}

func (pg *Client) batchLoadTags(ctx context.Context, ids []string) (map[string][]string, error) {
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

func (pg *Client) batchLoadSkills(ctx context.Context, ids []string) (map[string][]domain.EmployeeSkill, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT employee_id, skill, description FROM employee_skills WHERE employee_id = ANY($1)",
		ids)
	if err != nil {
		return nil, fmt.Errorf("batch load skills: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]domain.EmployeeSkill)
	for rows.Next() {
		var empID string
		var s domain.EmployeeSkill
		if err := rows.Scan(&empID, &s.Skill, &s.Description); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		result[empID] = append(result[empID], s)
	}
	return result, nil
}

func (pg *Client) GetEmployee(ctx context.Context, id string) (*domain.Employee, error) {
	var emp domain.Employee
	var adapterConfig []byte
	err := pg.pool.QueryRow(ctx, `
		SELECT e.id, e.name, e.title, e.role, e.backstory, e.avatar_url,
		       e.adapter_type, e.adapter_config, e.monthly_budget,
		       e.created_at, e.updated_at, r.manager_id
		FROM employees e
		LEFT JOIN employee_reporting r ON r.employee_id = e.id
		WHERE e.id = $1
	`, id).Scan(&emp.ID, &emp.Name, &emp.Title, &emp.Role, &emp.Backstory,
		&emp.AvatarURL, &emp.AdapterType, &adapterConfig, &emp.MonthlyBudget,
		&emp.CreatedAt, &emp.UpdatedAt, &emp.ManagerID)
	if err != nil {
		return nil, fmt.Errorf("get employee: %w", err)
	}
	emp.AdapterConfig = make(map[string]any)
	if len(adapterConfig) > 0 {
		if err := json.Unmarshal(adapterConfig, &emp.AdapterConfig); err != nil {
			return nil, fmt.Errorf("decode adapter config for employee %s: %w", emp.ID, err)
		}
	}

	emp.Models = []domain.EmployeeModel{}
	emp.Skills = []domain.EmployeeSkill{}
	emp.Tags = []string{}
	emp.Reports = []domain.EmployeeBrief{}

	batch := &pgx.Batch{}
	batch.Queue("SELECT model_id, purpose FROM employee_models WHERE employee_id = $1", id)
	batch.Queue("SELECT skill, description FROM employee_skills WHERE employee_id = $1", id)
	batch.Queue("SELECT tag FROM employee_tags WHERE employee_id = $1 ORDER BY tag", id)
	batch.Queue(`SELECT e.id, e.name, e.title, e.role FROM employees e
		JOIN employee_reporting r ON r.employee_id = e.id WHERE r.manager_id = $1`, id)
	br := pg.pool.SendBatch(ctx, batch)
	defer br.Close()

	if modelRows, err := br.Query(); err == nil {
		for modelRows.Next() {
			var m domain.EmployeeModel
			if err := modelRows.Scan(&m.ModelID, &m.Purpose); err == nil {
				emp.Models = append(emp.Models, m)
			}
		}
		modelRows.Close()
	}
	if skillRows, err := br.Query(); err == nil {
		for skillRows.Next() {
			var s domain.EmployeeSkill
			if err := skillRows.Scan(&s.Skill, &s.Description); err == nil {
				emp.Skills = append(emp.Skills, s)
			}
		}
		skillRows.Close()
	}
	if tagRows, err := br.Query(); err == nil {
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err == nil {
				emp.Tags = append(emp.Tags, tag)
			}
		}
		tagRows.Close()
	}
	if reportRows, err := br.Query(); err == nil {
		for reportRows.Next() {
			var b domain.EmployeeBrief
			if err := reportRows.Scan(&b.ID, &b.Name, &b.Title, &b.Role); err == nil {
				emp.Reports = append(emp.Reports, b)
			}
		}
		reportRows.Close()
	}

	return &emp, nil
}

func (pg *Client) CreateEmployee(ctx context.Context, emp *domain.Employee) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx, "create employee")

	adapterType := emp.AdapterType
	if adapterType == "" {
		adapterType = "internal_llm"
	}
	adapterConfig, _ := json.Marshal(emp.AdapterConfig)
	if emp.AdapterConfig == nil {
		adapterConfig = []byte("{}")
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO employees (name, title, role, backstory, avatar_url, adapter_type, adapter_config, monthly_budget)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, emp.Name, emp.Title, emp.Role, emp.Backstory, emp.AvatarURL,
		adapterType, adapterConfig, emp.MonthlyBudget).Scan(
		&emp.ID, &emp.CreatedAt, &emp.UpdatedAt)
	emp.AdapterType = adapterType
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

func (pg *Client) insertRelated(ctx context.Context, tx pgx.Tx, empID string, models []domain.EmployeeModel, skills []domain.EmployeeSkill, tags []string) error {
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

func (pg *Client) UpdateEmployee(ctx context.Context, id string, emp *domain.Employee) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx, "update employee")

	adapterType := emp.AdapterType
	if adapterType == "" {
		adapterType = "internal_llm"
	}
	adapterConfigJSON, _ := json.Marshal(emp.AdapterConfig)
	if emp.AdapterConfig == nil {
		adapterConfigJSON = []byte("{}")
	}

	_, err = tx.Exec(ctx, `
		UPDATE employees
		SET name=$1, title=$2, role=$3, backstory=$4, avatar_url=$5,
		    adapter_type=$6, adapter_config=$7, monthly_budget=$8, updated_at=NOW()
		WHERE id=$9
	`, emp.Name, emp.Title, emp.Role, emp.Backstory, emp.AvatarURL,
		adapterType, adapterConfigJSON, emp.MonthlyBudget, id)
	if err != nil {
		return fmt.Errorf("update employee: %w", err)
	}

	for _, table := range []string{"employee_models", "employee_skills", "employee_tags"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE employee_id=$1", id); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	if err := pg.insertRelated(ctx, tx, id, emp.Models, emp.Skills, emp.Tags); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, "DELETE FROM employee_reporting WHERE employee_id=$1", id); err != nil {
		return fmt.Errorf("clear reporting: %w", err)
	}
	if emp.ManagerID != nil && *emp.ManagerID != "" {
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)",
			id, *emp.ManagerID); err != nil {
			return fmt.Errorf("update reporting: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (pg *Client) DeleteEmployee(ctx context.Context, id string) error {
	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer rollbackTx(ctx, tx, "delete employee")

	var managerID *string
	err = tx.QueryRow(ctx, "SELECT manager_id FROM employee_reporting WHERE employee_id=$1", id).Scan(&managerID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load manager for %s: %w", id, err)
	}

	if managerID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE employee_reporting SET manager_id=$1
			WHERE manager_id=$2
		`, *managerID, id); err != nil {
			return fmt.Errorf("reassign reports: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, "DELETE FROM employee_reporting WHERE manager_id=$1", id); err != nil {
			return fmt.Errorf("clear reports: %w", err)
		}
	}

	_, err = tx.Exec(ctx, "DELETE FROM employees WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("delete employee: %w", err)
	}

	return tx.Commit(ctx)
}

func (pg *Client) SetManager(ctx context.Context, employeeID, managerID string) error {
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

func (pg *Client) CountEmployees(ctx context.Context) (int, error) {
	var count int
	err := pg.pool.QueryRow(ctx, "SELECT COUNT(*) FROM employees").Scan(&count)
	return count, err
}

func (pg *Client) backfillDefaultTags(ctx context.Context) {
	defaults := map[string][]string{
		"Elong": {"executive", "founder"},
		"Steve": {"manager", "founder"},
		"Linas": {"manager", "founder"},
	}
	for name, tags := range defaults {
		var id string
		err := pg.pool.QueryRow(ctx, "SELECT id FROM employees WHERE name=$1", name).Scan(&id)
		if err != nil {
			continue
		}
		for _, tag := range tags {
			pg.pool.Exec(ctx,
				"INSERT INTO employee_tags (employee_id, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING",
				id, tag)
		}
	}
}

func (pg *Client) SeedDefaultEmployees(ctx context.Context) error {
	count, err := pg.CountEmployees(ctx)
	if err != nil {
		return fmt.Errorf("count employees: %w", err)
	}
	if count > 0 {
		pg.backfillDefaultTags(ctx)
		return nil
	}

	defaults, err := seed.DefaultEmployees()
	if err != nil {
		return fmt.Errorf("load seed employees: %w", err)
	}

	tx, err := pg.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed tx: %w", err)
	}
	defer rollbackTx(ctx, tx, "seed employees")

	nameToID := make(map[string]string, len(defaults))

	for _, d := range defaults {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO employees (name, title, role, backstory)
			VALUES ($1, $2, $3, $4) RETURNING id
		`, d.Name, d.Title, d.Role, d.Backstory).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed employee %s: %w", d.Name, err)
		}
		nameToID[d.Name] = id

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

	for _, d := range defaults {
		if d.Manager == "" {
			continue
		}
		managerID, ok := nameToID[d.Manager]
		if !ok {
			return fmt.Errorf("seed reporting: manager %q not found for %s", d.Manager, d.Name)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO employee_reporting (employee_id, manager_id) VALUES ($1, $2)",
			nameToID[d.Name], managerID); err != nil {
			return fmt.Errorf("seed reporting for %s: %w", d.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}

	slog.Info("default employees seeded", "count", len(defaults))
	return nil
}
