package harness

import "testing"

func TestSystemPromptBans(t *testing.T) {
	// The system prompt must explicitly ban the behaviors that
	// would generate huge output / unsafe edits.
	mustContain := []string{
		"cat",
		"heredoc",
		"vim", "top",
		"sed -n", "grep -n", "sed -i", "perl -pi",
		"wc -l",
		"GOAL.md",
		"pipefail",
	}
	for _, want := range mustContain {
		if !contains(SystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestSystemPromptIsStatic(t *testing.T) {
	// Sanity: the prompt should not contain anything that looks
	// like a per-task value.
	for _, bad := range []string{"$", "{{", "%s", "%d"} {
		if contains(SystemPrompt, bad) {
			t.Errorf("system prompt contains templating %q; should be static", bad)
		}
	}
}

func TestSystemPromptIsShort(t *testing.T) {
	// Hard cap: 4 KB. The prompt is meant to be cache-friendly;
	// bloat is a bug.
	if len(SystemPrompt) > 4096 {
		t.Errorf("system prompt too long: %d bytes (cap 4096)", len(SystemPrompt))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
