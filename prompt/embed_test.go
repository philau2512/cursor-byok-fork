package prompt

import (
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
