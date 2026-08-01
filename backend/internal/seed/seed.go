// Package seed holds the default-employee seed data (plan 6.6). The data
// lives in employees.yaml (embedded at build time so the binary stays
// self-contained); this file is just the schema and loader.
package seed

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed employees.yaml
var employeesYAML []byte

type Employee struct {
	Name      string   `yaml:"name"`
	Title     string   `yaml:"title"`
	Role      string   `yaml:"role"`
	Backstory string   `yaml:"backstory"`
	Skills    []Skill  `yaml:"skills"`
	Tags      []string `yaml:"tags"`
	// Manager is the Name of another seed employee ("" for the root).
	Manager string `yaml:"manager,omitempty"`
}

type Skill struct {
	Skill       string `yaml:"skill"`
	Description string `yaml:"description"`
}

// DefaultEmployees parses the embedded employees.yaml.
func DefaultEmployees() ([]Employee, error) {
	var out []Employee
	if err := yaml.Unmarshal(employeesYAML, &out); err != nil {
		return nil, fmt.Errorf("parse embedded employees.yaml: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("embedded employees.yaml contains no employees")
	}
	return out, nil
}
