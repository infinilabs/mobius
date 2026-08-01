package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ValidateProjectPath(relativePath string) error {
	if filepath.IsAbs(relativePath) {
		return fmt.Errorf("absolute paths not allowed")
	}
	// Reject any ".." path *segment* (real traversal) while allowing ".." inside a
	// filename (e.g. "my..file.txt"). A bare substring check false-positives on
	// the latter; a post-Clean prefix check alone misses "a/../../b".
	for _, seg := range strings.Split(relativePath, "/") {
		if seg == ".." {
			return fmt.Errorf("path traversal not allowed")
		}
	}
	cleaned := filepath.Clean(relativePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes project root")
	}
	return nil
}

// ResolveWithinRoot validates rel lexically, joins it under root, and resolves
// symlinks on the nearest existing ancestor so a pre-existing symlink cannot
// redirect the final target outside root. It returns the absolute path to use
// for the actual file operation. All agent file tools go through this.
func ResolveWithinRoot(root, rel string) (string, error) {
	if err := ValidateProjectPath(rel); err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.Clean(rel))

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolvedRoot = filepath.Clean(root) // root may not exist yet (fresh project)
	}

	// Walk up to the nearest existing ancestor and resolve its symlinks.
	check := full
	for {
		if r, rerr := filepath.EvalSymlinks(check); rerr == nil {
			check = r
			break
		}
		parent := filepath.Dir(check)
		if parent == check {
			check = filepath.Clean(full)
			break
		}
		check = parent
	}

	if check != resolvedRoot && !strings.HasPrefix(check, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project root")
	}
	return full, nil
}
