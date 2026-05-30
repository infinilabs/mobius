package main

import (
	"testing"
)

// ---- EventConfig defaults ----

func TestEventConfigApplyDefaults(t *testing.T) {
	var ec EventConfig
	ec.applyDefaults()
	if ec.BufferSize == 0 || ec.BatchSize == 0 || ec.FlushIntervalS == 0 ||
		ec.RetentionDays == 0 || ec.ArchiveCron == "" {
		t.Errorf("applyDefaults left a zero field: %+v", ec)
	}

	custom := EventConfig{BufferSize: 1, BatchSize: 2, FlushIntervalS: 3, RetentionDays: 4, ArchiveCron: "* * * * *"}
	custom.applyDefaults()
	if custom.BufferSize != 1 || custom.ArchiveCron != "* * * * *" {
		t.Errorf("applyDefaults overrode explicit values: %+v", custom)
	}
}

// ---- skill markdown: build then parse must round-trip ----

func TestSkillMD_RoundTrip(t *testing.T) {
	md := buildMobiusSkillMD("rust", `desc with "quotes"`, "1.2", "coding", []string{"a", "b"}, "BODY TEXT")
	fm, body, err := parseSkillMD([]byte(md))
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
	fm, got, err := parseSkillMD([]byte(body))
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
	fm, _, err := parseSkillMD([]byte(in))
	if err != nil || fm != nil {
		t.Errorf("unterminated frontmatter mishandled: fm=%v err=%v", fm, err)
	}
}
