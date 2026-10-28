package forwarder

import (
	"encoding/json"
	"testing"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
)

func TestParseSubagentModelOverridesRegistersExploreAliases(t *testing.T) {
	items := []*agentv1.SubagentModelOverride{
		{
			SubagentType: "Explorer",
			Selection: &agentv1.SubagentModelOverride_Model{
				Model: &agentv1.RequestedModel{
					ModelId: "9antigravity-medium",
				},
			},
		},
	}

	parsed := parseSubagentModelOverrides(items)
	if len(parsed.Overrides) == 0 {
		t.Fatalf("expected overrides to be populated")
	}

	// Should be accessible by "Explorer" and normalized "explore"
	if override, ok := parsed.Overrides["Explorer"]; !ok || override.ModelID != "9antigravity-medium" {
		t.Errorf("expected override for Explorer, got %+v", override)
	}
	if override, ok := parsed.Overrides["explore"]; !ok || override.ModelID != "9antigravity-medium" {
		t.Errorf("expected normalized alias override for explore, got %+v", override)
	}
}

func TestTaskSubagentModelResolutionUsesExploreOverrideForSubagents(t *testing.T) {
	overrides := map[string]runtimecore.SubagentModelOverrideSelection{
		"explore": {
			SubagentType: "explore",
			Selection:    "model",
			ModelID:      "9antigravity-medium",
		},
	}

	tests := []struct {
		name                 string
		subagentType         string
		taskModel            string
		wantEffectiveModelID string
		wantOverrideHit      bool
		wantMatchedKey       string
	}{
		{
			name:                 "custom subagent advisor inherits explore model",
			subagentType:         "advisor",
			taskModel:            "",
			wantEffectiveModelID: "9antigravity-medium",
			wantOverrideHit:      true,
			wantMatchedKey:       "explore",
		},
		{
			name:                 "custom subagent tester with fast model inherits explore model",
			subagentType:         "tester",
			taskModel:            "fast",
			wantEffectiveModelID: "9antigravity-medium",
			wantOverrideHit:      true,
			wantMatchedKey:       "explore",
		},
		{
			name:                 "explore subagent uses explore model",
			subagentType:         "explore",
			taskModel:            "",
			wantEffectiveModelID: "9antigravity-medium",
			wantOverrideHit:      true,
			wantMatchedKey:       "explore",
		},
		{
			name:                 "generalPurpose subagent falls back to explore model",
			subagentType:         "generalPurpose",
			taskModel:            "",
			wantEffectiveModelID: "9antigravity-medium",
			wantOverrideHit:      true,
			wantMatchedKey:       "explore",
		},
		{
			name:                 "unspecified subagent falls back to explore model",
			subagentType:         "",
			taskModel:            "",
			wantEffectiveModelID: "9antigravity-medium",
			wantOverrideHit:      true,
			wantMatchedKey:       "explore",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{
				"prompt": "do work",
			}
			if tc.subagentType != "" {
				args["subagent_type"] = tc.subagentType
			}
			if tc.taskModel != "" {
				args["model"] = tc.taskModel
			}
			argsJSON, _ := json.Marshal(args)
			invocation := runtimecore.ToolInvocation{
				ToolName: "Task",
				CallID:   "call-123",
				ArgsJSON: argsJSON,
			}

			payload := taskSubagentModelResolutionPayload(invocation, "parent-claude-3-7-sonnet", overrides)
			if payload == nil {
				t.Fatalf("expected non-nil resolution payload")
			}
			if payload["effective_model_id"] != tc.wantEffectiveModelID {
				t.Errorf("effective_model_id = %v, want %v", payload["effective_model_id"], tc.wantEffectiveModelID)
			}
			if payload["override_hit"] != tc.wantOverrideHit {
				t.Errorf("override_hit = %v, want %v", payload["override_hit"], tc.wantOverrideHit)
			}
			if payload["matched_subagent_type"] != tc.wantMatchedKey {
				t.Errorf("matched_subagent_type = %v, want %v", payload["matched_subagent_type"], tc.wantMatchedKey)
			}

			// Check display rewriting
			rewritten := rewriteTaskInvocationModelForDisplay(invocation, "parent-claude-3-7-sonnet", overrides)
			var rewrittenArgs map[string]any
			if err := json.Unmarshal(rewritten.ArgsJSON, &rewrittenArgs); err != nil {
				t.Fatalf("failed to unmarshal rewritten args: %v", err)
			}
			if rewrittenArgs["model"] != tc.wantEffectiveModelID {
				t.Errorf("rewritten args[model] = %v, want %v", rewrittenArgs["model"], tc.wantEffectiveModelID)
			}
		})
	}
}

func TestSubagentTypeStringParsingCaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		isExlore bool
	}{
		{"explore", true},
		{"Explore", true},
		{"EXPLORE", true},
		{"explorer", true},
		{"Explorer", true},
	}

	for _, tc := range tests {
		res := subagentTypeFromString(tc.input)
		if res == nil || res.GetExplore() == nil {
			t.Errorf("subagentTypeFromString(%q) did not return Explore type", tc.input)
		}
	}
}