package httpapi

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---- skill_sync extraction helpers ----

func TestExtractHermesCategory(t *testing.T) {
	sep := string(filepath.Separator)
	tests := map[string]string{
		"coding" + sep + "rust.md": "coding",
		"rust.md":                  "general", // no directory component
		"":                         "general",
	}
	for rel, want := range tests {
		if got := extractHermesCategory(rel); got != want {
			t.Errorf("extractHermesCategory(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestExtractHermesTags(t *testing.T) {
	var hfm hermesFrontmatter
	hfm.Metadata.Hermes.Tags = []string{"Foo", "  Bar  "}

	tags := extractHermesTags(hfm, "hermes", "coding")
	// Expect deduped, lowercased, trimmed, sorted: source + category + tags.
	want := []string{"bar", "coding", "foo", "hermes"}
	if strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Errorf("extractHermesTags = %v, want %v", tags, want)
	}

	// category "general" must not become a tag.
	got := extractHermesTags(hermesFrontmatter{}, "src", "general")
	for _, tag := range got {
		if tag == "general" {
			t.Error("\"general\" category should not be added as a tag")
		}
	}
}
