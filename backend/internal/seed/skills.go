// Skill-assignment defaults for seeded employees (plan 6.2): which
// SKILL.md documents each seeded employee starts with.
package seed

func FounderSkillDefaults() map[string][]string {
	return map[string][]string{
		"Elong": {
			"task-decomposition",
			"planning-and-task-breakdown",
			"idea-refine",
			"spec-driven-development",
			"shipping-and-launch",
			"one-three-one-rule",
		},
		"Steve": {
			"writing-plans",
			"code-review",
			"frontend-ui-engineering",
			"web-design-guidelines",
			"interview-me",
			"documentation-and-adrs",
			"frontend-design",
		},
		"Linas": {
			"systematic-debugging",
			"test-driven-development",
			"codebase-inspection",
			"spike",
			"incremental-implementation",
			"performance-optimization",
			"code-simplification",
			"api-and-interface-design",
			"security-and-hardening",
			"source-driven-development",
			"mcp-builder",
			"playable-orchestration",
		},
	}
}

// EmployeeSkillDefaults binds on-disk SKILL.md guidance to non-founder employees
// by name. Tool ACCESS is tag-gated (e.g. media_tagger), but the SKILL.md manual
// only reaches the prompt via skill_assignments; without an entry here the
// employee has the tools but no manual. Kept separate from FounderSkillDefaults
// so the founder-only ResetEmployeeSkills path stays correct.
func EmployeeSkillDefaults() map[string][]string {
	return map[string][]string{
		"Creative Tagger": {
			"video-tagging",
		},
		"Asset Watermarker": {
			"asset-watermarking",
		},
		"Playable Planner": {
			"playable-planning",
		},
		"Playable Designer": {
			"playable-design",
		},
		"Playable Developer": {
			"playable-development",
		},
		"Playable Reviewer": {
			"playable-review",
		},
		"Playable Publisher": {
			"playable-publishing",
		},
	}
}
