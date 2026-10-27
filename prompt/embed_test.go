package prompt

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestReadPromptLoadsCleanModePrompts(t *testing.T) {
	for _, mode := range []Mode{ModeAgent, ModeAsk, ModePlan, ModeDebug, ModeMultitask, ModeSubagent} {
		text, err := ReadPrompt(mode)
		if err != nil {
			t.Fatalf("ReadPrompt(%s): %v", mode, err)
		}
		if len(strings.TrimSpace(text)) == 0 {
			t.Fatalf("ReadPrompt(%s) is empty", mode)
		}
	}

	compaction, err := ReadCompactionPrompt()
	if err != nil {
		t.Fatalf("ReadCompactionPrompt: %v", err)
	}
	if len(strings.TrimSpace(compaction)) == 0 {
		t.Fatal("compaction prompt is empty")
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
		"## 1. Executive Summary",
		"## 2. In-Scope Defects",
		"## 3. Pre-existing Observations",
		"## 4. Actionable Checklist",
		"<core_principles>",
		"Anti-Sycophancy & Technical Truth",
		"Scope Discipline & Minimal Patch",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Subagent prompt missing contract or guideline %q", required)
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

func TestToolsCatalogSingleSharedFile(t *testing.T) {
	catalogBytes, err := ReadToolsCatalog()
	if err != nil {
		t.Fatalf("ReadToolsCatalog() error: %v", err)
	}
	var catalog struct {
		Tools    []json.RawMessage          `json:"tools"`
		Variants map[string]json.RawMessage `json:"variants"`
	}
	if err := json.Unmarshal(catalogBytes, &catalog); err != nil {
		t.Fatalf("Unmarshal tools catalog error: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatal("tools catalog has 0 tools")
	}
	if len(catalog.Variants) == 0 {
		t.Fatal("tools catalog has 0 variants")
	}

	for _, mode := range []Mode{ModeAgent, ModeAsk, ModePlan, ModeDebug, ModeMultitask, ModeSubagent} {
		path, err := ToolsPath(mode)
		if err != nil {
			t.Fatalf("ToolsPath(%s) error: %v", mode, err)
		}
		if !strings.HasPrefix(path, "modes/") || !strings.HasSuffix(path, ".json") {
			t.Fatalf("ToolsPath(%s) = %q, want modes/<mode>.json", mode, path)
		}
		raw, err := ReadTools(mode)
		if err != nil {
			t.Fatalf("ReadTools(%s) error: %v", mode, err)
		}
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err != nil {
			t.Fatalf("Unmarshal mode %s tools error: %v", mode, err)
		}
		if len(tools) == 0 {
			t.Fatalf("mode %s resolved 0 tools", mode)
		}
	}
}


