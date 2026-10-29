package forwarder

import (
	"encoding/json"
	"reflect"
	"testing"

	"cursor/gen/agentv1"
	modeladapter "cursor/internal/backend/agent/model"
)

func TestDefaultPromptCompilerPreservesLosslessReplayFixture(t *testing.T) {
	conversation := losslessReplayFixture(t)
	projector := NewHistoryProjector()
	compiler := NewPromptCompiler(projector, NewToolCatalog(), nil, nil)

	expectedReplay, err := projector.ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("ProjectPromptReplay() error = %v", err)
	}
	compiled, err := compiler.Compile(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "", "test-model")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(compiled.Messages) != len(expectedReplay)+1 {
		t.Fatalf("compiled message count = %d, want system prompt plus %d replay messages", len(compiled.Messages), len(expectedReplay))
	}
	if compiled.Messages[0].Role != "system" {
		t.Fatalf("first compiled message role = %q, want system", compiled.Messages[0].Role)
	}
	assertEquivalentProviderMessages(t, compiled.Messages[1:], expectedReplay)

	replayedAgain, err := projector.ProjectPromptReplay(conversation)
	if err != nil {
		t.Fatalf("second ProjectPromptReplay() error = %v", err)
	}
	compiledAgain, err := compiler.Compile(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "", "test-model")
	if err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}
	assertEquivalentProviderMessages(t, replayedAgain, expectedReplay)
	assertEquivalentProviderMessages(t, compiledAgain.Messages[1:], expectedReplay)

	if compiled.StableMessageCount != len(expectedReplay)-1 {
		t.Fatalf("StableMessageCount = %d, want %d historical messages before the current turn", compiled.StableMessageCount, len(expectedReplay)-1)
	}
}

func BenchmarkDefaultPromptCompilerLosslessReplay(b *testing.B) {
	conversation := losslessReplayFixture(b)
	compiler := NewPromptCompiler(NewHistoryProjector(), NewToolCatalog(), nil, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		compiled, err := compiler.Compile(conversation, agentv1.AgentMode_AGENT_MODE_AGENT, "", "test-model")
		if err != nil {
			b.Fatalf("Compile() error = %v", err)
		}
		if len(compiled.Messages) == 0 {
			b.Fatal("Compile() returned no messages")
		}
	}
}

func losslessReplayFixture(t testing.TB) *ConversationFile {
	t.Helper()
	entries := []HistoryEntry{
		losslessReplayMessageEntry(t, 1, "request-1", modeladapter.Message{
			Role:    "user",
			Content: "Investigate the compiler latency without dropping prior evidence.",
		}),
		losslessReplayMessageEntry(t, 1, "request-1", modeladapter.Message{
			Role:                     "assistant",
			Content:                  "I will inspect the history projector first.",
			ReasoningContent:         "Need preserve the exact provider-visible sequence.",
			ReasoningSignature:       "historical-signature",
			ReasoningSignatureSource: modeladapter.ReasoningSignatureSourceAnthropic,
		}),
		losslessReplayMessageEntry(t, 1, "request-1", modeladapter.Message{
			Role: "assistant",
			ToolCalls: []modeladapter.ToolCallDescriptor{{
				ID:   "call-read-1",
				Type: "function",
				Function: modeladapter.ToolCallFunctionShape{
					Name:      "Read",
					Arguments: `{"path":"internal/backend/forwarder/compiler.go"}`,
				},
			}},
			ReasoningContent:         "Read the compilation boundary before changing it.",
			ReasoningSignature:       "tool-signature",
			ReasoningSignatureSource: modeladapter.ReasoningSignatureSourceAnthropic,
		}),
		losslessReplayMessageEntry(t, 1, "request-1", modeladapter.Message{
			Role:       "tool",
			Name:       "Read",
			ToolCallID: "call-read-1",
			Content:    "func (compiler *DefaultPromptCompiler) Compile(...) { /* full historical tool result */ }",
		}),
	}
	for turn := int64(2); turn <= 24; turn++ {
		requestID := "request-long-history"
		entries = append(entries,
			losslessReplayMessageEntry(t, turn, requestID, modeladapter.Message{
				Role:    "assistant",
				Content: "Historical implementation observation retained verbatim for replay baseline.",
			}),
			losslessReplayMessageEntry(t, turn, requestID, modeladapter.Message{
				Role:       "tool",
				Name:       "Grep",
				ToolCallID: "call-history",
				Content:    "historical tool result retained verbatim for lossless benchmark coverage",
			}),
		)
	}
	return testConversation(entries)
}

func losslessReplayMessageEntry(t testing.TB, turnSeq int64, requestID string, message modeladapter.Message) HistoryEntry {
	t.Helper()
	entry, ok, err := newModelMessageEntry(turnSeq, requestID, message)
	if err != nil {
		t.Fatalf("newModelMessageEntry() error = %v", err)
	}
	if !ok {
		t.Fatal("newModelMessageEntry() did not produce a replayable entry")
	}
	return entry
}

func assertEquivalentProviderMessages(t testing.TB, got []modeladapter.Message, want []modeladapter.Message) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal actual messages: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected messages: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider-visible replay changed\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
