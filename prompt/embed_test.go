package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommonPrefixAllowsHybridSingleSubagentDelegation(t *testing.T) {
	text, err := assetFS.ReadFile("common_prefix.md")
	if err != nil {
		t.Fatalf("read common prefix: %v", err)
	}
	for _, required := range []string{
		"Use a single Subagent",
		"bounded implementation or validation subtask",
		"follow-up can reuse its large task context",
		"materially reduces the primary agent's context growth",
		"Do not delegate simple lookups",
		"Structured Task Prompt Format",
		"Staged Execution (Phased Pipelines)",
		"Parent Integration Verification Gate",
		"Handling BLOCKED or PARTIAL Outcomes",
		"resume:",
	} {
		if !strings.Contains(string(text), required) {
			t.Fatalf("common prefix missing subagent policy %q", required)
		}
	}
}

func TestDebugEvidenceGateScopesInstrumentationToUnresolvedRootCauses(t *testing.T) {
	text, err := assetFS.ReadFile("debug/system_reminder_initial.txt")
	if err != nil {
		t.Fatalf("read debug system reminder: %v", err)
	}
	for _, required := range []string{
		"when the root cause is unresolved",
		"If the Evidence gate is satisfied",
		"at least 1 log is required",
		"before/after comparison of the relevant runtime evidence",
	} {
		if !strings.Contains(string(text), required) {
			t.Fatalf("debug prompt missing evidence-gate contract %q", required)
		}
	}
}

func TestReadPromptPrependsCommonPrefixExactlyOnceForSharedModes(t *testing.T) {
	prefix, err := assetFS.ReadFile("common_prefix.md")
	if err != nil {
		t.Fatalf("read common prefix: %v", err)
	}
	marker := "You are an exceptionally pragmatic and efficient software engineer."
	if !strings.Contains(string(prefix), marker) {
		t.Fatal("common prefix marker is missing")
	}

	for _, mode := range []Mode{ModeAgent, ModePlan, ModeAsk, ModeMultitask} {
		text, err := ReadPrompt(mode)
		if err != nil {
			t.Fatalf("ReadPrompt(%s): %v", mode, err)
		}
		if count := strings.Count(text, marker); count != 1 {
			t.Fatalf("ReadPrompt(%s) contains common prefix marker %d times, want 1", mode, count)
		}
	}
}

func TestReadPromptKeepsSpecializedPromptsIsolated(t *testing.T) {
	prefixMarker := "You are an exceptionally pragmatic and efficient software engineer."
	for _, mode := range []Mode{ModeDebug, ModeSubagent} {
		text, err := ReadPrompt(mode)
		if err != nil {
			t.Fatalf("ReadPrompt(%s): %v", mode, err)
		}
		if strings.Contains(text, prefixMarker) {
			t.Fatalf("ReadPrompt(%s) unexpectedly includes common prefix", mode)
		}
	}

	compaction, err := ReadCompactionPrompt()
	if err != nil {
		t.Fatalf("ReadCompactionPrompt: %v", err)
	}
	if strings.Contains(compaction, prefixMarker) {
		t.Fatal("compaction prompt unexpectedly includes common prefix")
	}
}

func TestAskPromptContractForbidsMutations(t *testing.T) {
	text, err := ReadPrompt(ModeAsk)
	if err != nil {
		t.Fatalf("ReadPrompt(ask): %v", err)
	}
	for _, required := range []string{
		"must not make any edits",
		"run any non-read-only tools",
		"must not implement it yourself",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Ask prompt missing contract %q", required)
		}
	}
}

func TestAllToolCatalogsAreValidJSONAndConsistent(t *testing.T) {
	for _, mode := range []Mode{ModeAgent, ModeAsk, ModePlan, ModeDebug, ModeMultitask, ModeSubagent} {
		raw, err := ReadTools(mode)
		if err != nil {
			t.Fatalf("ReadTools(%s): %v", mode, err)
		}
		var tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err != nil {
			t.Fatalf("Unmarshal tools for mode %s: %v", mode, err)
		}
		if len(tools) == 0 {
			t.Fatalf("tools for mode %s is empty", mode)
		}
		for _, tool := range tools {
			if tool.Function.Name == "" {
				t.Fatalf("tool in mode %s has empty function name", mode)
			}
			if tool.Function.Description == "" {
				t.Fatalf("tool %s in mode %s has empty description", tool.Function.Name, mode)
			}
		}
	}
}

func TestSubagentPromptContainsHandoffContractAndSafetyGuidelines(t *testing.T) {
	text, err := ReadPrompt(ModeSubagent)
	if err != nil {
		t.Fatalf("ReadPrompt(subagent): %v", err)
	}
	for _, required := range []string{
		"<handoff_return_contract>",
		"**Status**",
		"**Changes / Findings**",
		"**Evidence & Verification**",
		"# Editing constraints",
		"# Tool & Terminal guidelines",
		"Never touch files outside your delegated task",
		"Prefer native tools",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Subagent prompt missing contract or safety guideline %q", required)
		}
	}
}

func TestDebugPromptAndReminderContainSubagentPolicy(t *testing.T) {
	reminder, err := ReadDebugSystemReminder(true)
	if err != nil {
		t.Fatalf("ReadDebugSystemReminder(true): %v", err)
	}
	for _, required := range []string{
		"<subagent_delegation_in_debug_mode>",
		"Heavy Noise & Log Trace Isolation",
		"Parallel Hypothesis Testing",
		"Context Reuse",
	} {
		if !strings.Contains(reminder, required) {
			t.Fatalf("Debug system reminder missing subagent policy %q", required)
		}
	}

	continuingReminder, err := ReadDebugSystemReminder(false)
	if err != nil {
		t.Fatalf("ReadDebugSystemReminder(false): %v", err)
	}
	if !strings.Contains(continuingReminder, "Subagent delegation") {
		t.Fatalf("Debug continuing reminder missing subagent delegation note")
	}
}

func TestSubagentToolsCatalogSchema(t *testing.T) {
	raw, err := ReadTools(ModeSubagent)
	if err != nil {
		t.Fatalf("ReadTools(subagent): %v", err)
	}
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("Unmarshal subagent tools: %v", err)
	}
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Function.Name] = true
	}
	for _, required := range []string{"PatchEdit", "Write", "Delete", "Shell", "Glob", "Grep", "Read", "Ls", "ReadLints", "Task"} {
		if !toolNames[required] {
			t.Fatalf("subagent tools.json missing expected tool %q", required)
		}
	}
	for _, disallowed := range []string{"AskQuestion", "SwitchMode"} {
		if toolNames[disallowed] {
			t.Fatalf("subagent tools.json unexpectedly contains disallowed tool %q", disallowed)
		}
	}
}

