package tools

import (
	"os"
	"path/filepath"
	"sync"

	"mobius/internal/config"
	"mobius/internal/domain"
)

var projectMemoryLocks sync.Map

// ProjectLock returns the per-project RW mutex guarding mobius.md access.
func ProjectLock(projectName string) *sync.RWMutex {
	mu, _ := projectMemoryLocks.LoadOrStore(projectName, &sync.RWMutex{})
	return mu.(*sync.RWMutex)
}

// ReadProjectMemory returns the project's mobius.md contents ("" if absent).
func ReadProjectMemory(project *domain.Project, cfg *config.Config) string {
	mu := ProjectLock(project.Name)
	mu.RLock()
	defer mu.RUnlock()

	path := filepath.Join(project.RootDir(cfg.Projects.ProjectsDir), "mobius.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// ForgetProjectLock drops a deleted project's memory lock.
func ForgetProjectLock(projectName string) {
	projectMemoryLocks.Delete(projectName)
}
