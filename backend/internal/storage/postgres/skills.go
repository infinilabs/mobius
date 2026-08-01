package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"mobius/internal/domain"
	"mobius/internal/seed"
)

// PG operations for skill-employee assignments

func (pg *Client) AssignSkill(ctx context.Context, employeeID, skillID string) error {
	_, err := pg.pool.Exec(ctx,
		"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		employeeID, skillID)
	if err != nil {
		return fmt.Errorf("assign skill: %w", err)
	}
	return nil
}

func (pg *Client) UnassignSkill(ctx context.Context, employeeID, skillID string) error {
	_, err := pg.pool.Exec(ctx,
		"DELETE FROM skill_assignments WHERE employee_id=$1 AND skill_id=$2",
		employeeID, skillID)
	if err != nil {
		return fmt.Errorf("unassign skill: %w", err)
	}
	return nil
}

func (pg *Client) ListEmployeeSkillIDs(ctx context.Context, employeeID string) ([]string, error) {
	rows, err := pg.pool.Query(ctx,
		"SELECT skill_id FROM skill_assignments WHERE employee_id=$1 ORDER BY assigned_at",
		employeeID)
	if err != nil {
		return nil, fmt.Errorf("list employee skills: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan skill id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (pg *Client) SeedDefaultSkillAssignments(ctx context.Context, es SkillLookup) error {
	if es == nil {
		return nil
	}

	defaults := seed.FounderSkillDefaults()
	for empName, skillNames := range seed.EmployeeSkillDefaults() {
		defaults[empName] = append(defaults[empName], skillNames...)
	}

	for empName, skillNames := range defaults {
		var empID string
		err := pg.pool.QueryRow(ctx, "SELECT id FROM employees WHERE name=$1", empName).Scan(&empID)
		if err != nil {
			continue
		}

		for _, sn := range skillNames {
			skillID := domain.SkillIDFromName(sn)
			if _, lookupErr := es.GetSkill(ctx, skillID); lookupErr != nil {
				slog.Warn("default skill not in ES, skipping assignment", "employee", empName, "skill", sn, "id", skillID)
				continue
			}
			pg.pool.Exec(ctx,
				"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
				empID, skillID)
		}
	}

	rows, err := pg.pool.Query(ctx, "SELECT id, name FROM employees")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var empID, empName string
			if rows.Scan(&empID, &empName) != nil {
				continue
			}
			skillID := domain.SkillIDFromName(empName)
			if _, lookupErr := es.GetSkill(ctx, skillID); lookupErr != nil {
				continue
			}
			pg.pool.Exec(ctx,
				"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
				empID, skillID)
		}
	}

	slog.Info("default skill assignments seeded")
	return nil
}

func (pg *Client) ResetEmployeeSkills(ctx context.Context, es SkillLookup, employeeID string) error {
	if es == nil {
		return nil
	}

	var empName string
	err := pg.pool.QueryRow(ctx, "SELECT name FROM employees WHERE id=$1", employeeID).Scan(&empName)
	if err != nil {
		return fmt.Errorf("employee not found: %w", err)
	}

	var count int
	pg.pool.QueryRow(ctx, "SELECT count(*) FROM employee_tags WHERE employee_id=$1 AND tag='founder'", employeeID).Scan(&count)
	if count == 0 {
		return fmt.Errorf("reset is only available for founders")
	}

	defaults := seed.FounderSkillDefaults()

	skillNames, ok := defaults[empName]
	if !ok {
		return fmt.Errorf("no default skills defined for %s", empName)
	}

	pg.pool.Exec(ctx, "DELETE FROM skill_assignments WHERE employee_id=$1", employeeID)

	for _, sn := range skillNames {
		skillID := domain.SkillIDFromName(sn)
		if _, lookupErr := es.GetSkill(ctx, skillID); lookupErr != nil {
			continue
		}
		pg.pool.Exec(ctx,
			"INSERT INTO skill_assignments (employee_id, skill_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			employeeID, skillID)
	}

	slog.Info("employee skills reset to defaults", "employee", empName, "id", employeeID)
	return nil
}
