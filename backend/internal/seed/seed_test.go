package seed

import "testing"

// The embedded roster must parse and be internally consistent — a broken
// employees.yaml would only surface on the first boot against an empty
// database, which is exactly when nobody is watching.
func TestDefaultEmployees_ParsesAndIsConsistent(t *testing.T) {
	emps, err := DefaultEmployees()
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool, len(emps))
	for _, e := range emps {
		if e.Name == "" || e.Role == "" {
			t.Errorf("employee with empty name/role: %+v", e)
		}
		if names[e.Name] {
			t.Errorf("duplicate employee name %q", e.Name)
		}
		names[e.Name] = true
	}

	// Every manager reference must resolve, or SeedDefaultEmployees fails the
	// whole seed transaction at first boot.
	for _, e := range emps {
		if e.Manager != "" && !names[e.Manager] {
			t.Errorf("employee %q reports to unknown manager %q", e.Name, e.Manager)
		}
	}
}
