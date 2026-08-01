package httpapi

import (
	"encoding/json"
	"log/slog"
	"mobius/internal/domain"
	"net/http"
)

// domain.Employee, domain.EmployeeBrief, domain.EmployeeModel, domain.EmployeeSkill live in
// internal/domain (plan 6.1); see domain_aliases.go.

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

	var emp domain.Employee
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
		emp.Models = []domain.EmployeeModel{}
	}
	if emp.Skills == nil {
		emp.Skills = []domain.EmployeeSkill{}
	}
	if emp.Tags == nil {
		emp.Tags = []string{}
	}

	if err := h.pgClient.CreateEmployee(r.Context(), &emp); err != nil {
		writeError(w, "failed to create employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.esClient != nil {
		if err := h.esClient.IndexEmployee(r.Context(), &emp); err != nil {
			slog.Warn("ES index employee failed", "id", emp.ID, "error", err)
		}
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
	var emp domain.Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if emp.Models == nil {
		emp.Models = []domain.EmployeeModel{}
	}
	if emp.Skills == nil {
		emp.Skills = []domain.EmployeeSkill{}
	}
	if emp.Tags == nil {
		emp.Tags = []string{}
	}

	if err := h.pgClient.UpdateEmployee(r.Context(), id, &emp); err != nil {
		writeError(w, "failed to update employee: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updated, _ := h.pgClient.GetEmployee(r.Context(), id)
	if h.esClient != nil && updated != nil {
		if err := h.esClient.IndexEmployee(r.Context(), updated); err != nil {
			slog.Warn("ES index employee failed", "id", id, "error", err)
		}
	}
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

	if h.esClient != nil {
		if err := h.esClient.DeleteESEmployee(r.Context(), id); err != nil {
			slog.Warn("ES delete employee failed", "id", id, "error", err)
		}
		if err := h.esClient.DeleteEmployeeMemories(r.Context(), id); err != nil {
			slog.Warn("failed to clean up employee memories from ES", "employee_id", id, "error", err)
		}
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
		_, err := h.pgClient.Pool().Exec(r.Context(), "DELETE FROM employee_reporting WHERE employee_id=$1", id)
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
