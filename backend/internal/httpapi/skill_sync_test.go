package httpapi

import (
	"testing"

	"mobius/internal/skills"
)

func TestSkillMD_RoundTrip(t *testing.T) {
	md := buildMobiusSkillMD("rust", `desc with "quotes"`, "1.2", "coding", []string{"a", "b"}, "BODY TEXT")
	fm, body, err := skills.ParseSkillMD([]byte(md))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if fm == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if fm.Name != "rust" || fm.Version != "1.2" || fm.Category != "coding" {
		t.Errorf("frontmatter not round-tripped: %+v", fm)
	}
	if body != "BODY TEXT" {
		t.Errorf("body = %q, want %q", body, "BODY TEXT")
	}
}

func TestParseSkillMD_NoFrontmatter(t *testing.T) {
	body := "just content, no frontmatter"
	fm, got, err := skills.ParseSkillMD([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil {
		t.Error("expected nil frontmatter for plain content")
	}
	if got != body {
		t.Errorf("content = %q, want %q", got, body)
	}
}

func TestParseSkillMD_UnterminatedFrontmatter(t *testing.T) {
	// Opening fence but no closing "---" → treated as plain content, no error.
	in := "---\nname: x\nstill going"
	fm, _, err := skills.ParseSkillMD([]byte(in))
	if err != nil || fm != nil {
		t.Errorf("unterminated frontmatter mishandled: fm=%v err=%v", fm, err)
	}
}
