package promptengine

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"cursor/gen/agentv1"
)

func TestCompileReinjectsUserRulesAlongsideCompactedSummary(t *testing.T) {
	summary, err := proto.Marshal(&agentv1.ConversationSummary{Summary: "compacted handoff"})
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}

	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		ConversationState: &agentv1.ConversationStateStructure{
			Summary: summary,
		},
		RequestContext: &agentv1.RequestContext{
			Rules: []*agentv1.CursorRule{{Content: "Never modify config.yaml."}},
		},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	joined := make([]string, 0, len(compiled.Messages))
	for _, message := range compiled.Messages {
		joined = append(joined, message.Content)
	}
	prompt := strings.Join(joined, "\n")
	if !strings.Contains(prompt, "<conversation_summary>") {
		t.Fatal("compiled prompt does not include compacted summary")
	}
	if !strings.Contains(prompt, "<user_rule>Never modify config.yaml.</user_rule>") {
		t.Fatal("compiled prompt does not reinject the current user rule")
	}
}
