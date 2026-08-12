package llm

import "testing"

func TestPickLatestFlash(t *testing.T) {
	// The tagging remote model must track the newest BASE Flash generation and
	// never pick a variant (-lite/-image/-preview) — those have different
	// multimodal capabilities and pricing.
	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{
			"picks highest major.minor",
			[]string{"gemini-2.5-flash", "gemini-3.5-flash", "gemini-3.6-flash", "gemini-2.5-pro"},
			"gemini-3.6-flash",
		},
		{
			"major beats minor",
			[]string{"gemini-2.5-flash", "gemini-3-flash"},
			"gemini-3-flash",
		},
		{
			"ignores variants",
			[]string{"gemini-3.5-flash-lite", "gemini-3.1-flash-image", "gemini-3-flash-preview", "gemini-2.5-flash"},
			"gemini-2.5-flash",
		},
		{
			"none match",
			[]string{"gemini-2.5-pro", "claude-fable-5"},
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PickLatestFlash(c.ids); got != c.want {
				t.Errorf("PickLatestFlash(%v) = %q, want %q", c.ids, got, c.want)
			}
		})
	}
}
