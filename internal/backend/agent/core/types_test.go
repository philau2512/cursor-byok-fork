package runtimecore

import (
	"testing"
)

func TestLookupSubagentModelOverrideExploreFallback(t *testing.T) {
	overrides := map[string]SubagentModelOverrideSelection{
		"explore": {
			SubagentType: "explore",
			Selection:    "model",
			ModelID:      "claude-3-5-haiku-20241022",
		},
	}

	tests := []struct {
		name         string
		subagentType string
		wantFound    bool
		wantKey      string
		wantModelID  string
	}{
		{
			name:         "explore exact match",
			subagentType: "explore",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
		{
			name:         "Explorer case variation",
			subagentType: "Explorer",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
		{
			name:         "generalPurpose falls back to explore",
			subagentType: "generalPurpose",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
		{
			name:         "custom subagent advisor falls back to explore",
			subagentType: "advisor",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
		{
			name:         "custom subagent tester falls back to explore",
			subagentType: "tester",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
		{
			name:         "empty subagentType falls back to explore",
			subagentType: "",
			wantFound:    true,
			wantKey:      "explore",
			wantModelID:  "claude-3-5-haiku-20241022",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selection, matchedKey, ok := LookupSubagentModelOverride(overrides, tc.subagentType)
			if ok != tc.wantFound {
				t.Fatalf("LookupSubagentModelOverride(%q) ok = %v, want %v", tc.subagentType, ok, tc.wantFound)
			}
			if matchedKey != tc.wantKey {
				t.Errorf("LookupSubagentModelOverride(%q) matchedKey = %q, want %q", tc.subagentType, matchedKey, tc.wantKey)
			}
			if selection.ModelID != tc.wantModelID {
				t.Errorf("LookupSubagentModelOverride(%q) modelID = %q, want %q", tc.subagentType, selection.ModelID, tc.wantModelID)
			}
		})
	}
}

func TestLookupSubagentModelOverrideSpecificPrecedence(t *testing.T) {
	overrides := map[string]SubagentModelOverrideSelection{
		"explore": {
			SubagentType: "explore",
			Selection:    "model",
			ModelID:      "explore-model",
		},
		"advisor": {
			SubagentType: "advisor",
			Selection:    "model",
			ModelID:      "advisor-specific-model",
		},
		"shell": {
			SubagentType: "shell",
			Selection:    "disabled",
		},
	}

	// advisor should match its own override, not explore
	sel, key, ok := LookupSubagentModelOverride(overrides, "advisor")
	if !ok || key != "advisor" || sel.ModelID != "advisor-specific-model" {
		t.Errorf("expected advisor-specific-model, got key=%q sel=%+v ok=%v", key, sel, ok)
	}

	// shell should match its disabled override
	sel, key, ok = LookupSubagentModelOverride(overrides, "shell")
	if !ok || key != "shell" || sel.Selection != "disabled" {
		t.Errorf("expected shell disabled, got key=%q sel=%+v ok=%v", key, sel, ok)
	}

	// tester has no specific override, so falls back to explore
	sel, key, ok = LookupSubagentModelOverride(overrides, "tester")
	if !ok || key != "explore" || sel.ModelID != "explore-model" {
		t.Errorf("expected tester to fallback to explore, got key=%q sel=%+v ok=%v", key, sel, ok)
	}
}