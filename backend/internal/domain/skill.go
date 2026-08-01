package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	ContentHash string   `json:"content_hash,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// SkillIDFromName derives a skill's stable ID from its name (hex of the first
// 8 bytes of the SHA-256): deterministic across re-syncs from disk.
func SkillIDFromName(name string) string {
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:8])
}
