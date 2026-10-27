package forwarder

import (
	"encoding/json"
	"testing"

	modeladapter "cursor/internal/backend/agent/model"
)

func TestTokenBreakdownDetailedCategories(t *testing.T) {
	compiled := CompiledConversation{
		Messages: []modeladapter.Message{
			{
				Role:    "system",
				Content: "You are an AI assistant.\n<rules><rule>Always follow rules</rule></rules>",
			},
			{
				Role:    "user",
				Content: "Hello <agent_skills><skill>Coding skill</skill></agent_skills> <subagents><subagent>Subagent A</subagent></subagents> <mcp_meta_tools><mcp>MCP Server</mcp></mcp_meta_tools> <conversation_summary>Summary of previous turns</conversation_summary> How are you?",
			},
		},
		Tools: []json.RawMessage{
			json.RawMessage(`{"type":"function","function":{"name":"read_file"}}`),
			json.RawMessage(`{"type":"function","function":{"name":"call_mcp_tool"}}`),
		},
	}

	snapshot := estimateCheckpointPromptTokenBreakdown(compiled, true, 5000, 200000)
	if snapshot == nil {
		t.Fatal("expected snapshot to be non-nil")
	}

	categoryMap := make(map[string]uint32)
	for _, cat := range snapshot.GetCategories() {
		categoryMap[cat.GetId()] = cat.GetEstimatedTokens()
	}

	expectedCategories := []string{
		"system_prompt",
		"tools",
		"rules",
		"skills",
		"mcp",
		"subagents",
		"summarized_conversation",
		"conversation",
	}

	for _, id := range expectedCategories {
		tokens, ok := categoryMap[id]
		if !ok {
			t.Errorf("missing expected category %q", id)
		} else if tokens == 0 && id != "summarized_conversation" {
			t.Errorf("expected category %q to have > 0 tokens, got 0", id)
		}
	}

	// Verify authoritative sum matches usedTokens
	totalTokens := uint32(0)
	for _, cat := range snapshot.GetCategories() {
		totalTokens += cat.GetEstimatedTokens()
	}
	if totalTokens != 5000 {
		t.Fatalf("total categories token sum = %d, want 5000", totalTokens)
	}
}

func TestTokenBreakdownFitsAuthoritativeUsedTokens(t *testing.T) {
	compiled := CompiledConversation{
		Messages: []modeladapter.Message{
			{
				Role:    "system",
				Content: "System instruction",
			},
			{
				Role:    "user",
				Content: "User message here",
			},
		},
	}

	usedTokens := uint32(120000)
	snapshot := estimateCheckpointPromptTokenBreakdown(compiled, true, usedTokens, 356000)
	if snapshot == nil {
		t.Fatal("expected snapshot to be non-nil")
	}

	if snapshot.GetTotalUsedTokens() != usedTokens {
		t.Fatalf("TotalUsedTokens = %d, want %d", snapshot.GetTotalUsedTokens(), usedTokens)
	}

	sum := uint32(0)
	for _, c := range snapshot.GetCategories() {
		sum += c.GetEstimatedTokens()
	}
	if sum != usedTokens {
		t.Fatalf("Categories sum = %d, want %d (absorb into conversation without leaving 'Other')", sum, usedTokens)
	}
}

func TestTokenBreakdownStaticCatalogsNotMultipliedAcrossTurns(t *testing.T) {
	ruleChunk := "<rules><rule>Very long rule content repeated across turns</rule></rules>"
	skillChunk := "<agent_skills><skill>Very long skill content repeated across turns</skill></agent_skills>"

	compiledTurn1 := CompiledConversation{
		Messages: []modeladapter.Message{
			{
				Role:    "user",
				Content: ruleChunk + " " + skillChunk + " Turn 1",
			},
		},
	}

	compiledTurn2 := CompiledConversation{
		Messages: []modeladapter.Message{
			{
				Role:    "user",
				Content: ruleChunk + " " + skillChunk + " Turn 1",
			},
			{
				Role:    "assistant",
				Content: "Assistant response 1",
			},
			{
				Role:    "user",
				Content: ruleChunk + " " + skillChunk + " Turn 2",
			},
		},
	}

	snap1 := estimateCheckpointPromptTokenBreakdown(compiledTurn1, true, 0, 100000)
	snap2 := estimateCheckpointPromptTokenBreakdown(compiledTurn2, true, 0, 100000)

	var rules1, rules2, skills1, skills2 uint32
	for _, c := range snap1.GetCategories() {
		if c.GetId() == "rules" {
			rules1 = c.GetEstimatedTokens()
		}
		if c.GetId() == "skills" {
			skills1 = c.GetEstimatedTokens()
		}
	}
	for _, c := range snap2.GetCategories() {
		if c.GetId() == "rules" {
			rules2 = c.GetEstimatedTokens()
		}
		if c.GetId() == "skills" {
			skills2 = c.GetEstimatedTokens()
		}
	}

	if rules1 != rules2 {
		t.Fatalf("rules tokens changed across turns: turn1=%d, turn2=%d (should not multiply)", rules1, rules2)
	}
	if skills1 != skills2 {
		t.Fatalf("skills tokens changed across turns: turn1=%d, turn2=%d (should not multiply)", skills1, skills2)
	}
}