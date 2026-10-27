package promptengine

import (
	"os"
	"path/filepath"
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

func TestCompileInjectsMCPMetaToolsSection(t *testing.T) {
	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			McpMetaToolOptions: &agentv1.McpMetaToolOptions{
				Enabled: true,
				McpDescriptors: []*agentv1.McpDescriptor{
					{
						ServerIdentifier: "github-mcp",
						ServerName:       "GitHub",
						Tools: []*agentv1.McpToolDescriptor{
							{
								ToolName:        "create_issue",
								Description:     proto.String("Creates a new GitHub issue"),
								InputSchemaJson: proto.String(`{"type":"object","properties":{"title":{"type":"string"}}}`),
							},
						},
					},
					{
						ServerIdentifier: "user-fast-context",
						ServerName:       "fast-context",
						Tools: []*agentv1.McpToolDescriptor{
							{
								ToolName: "fast_context_search",
							},
						},
					},
				},
			},
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

	if !strings.Contains(prompt, "<mcp_meta_tools>") {
		t.Fatal("compiled prompt does not include <mcp_meta_tools>")
	}
	if !strings.Contains(prompt, `<mcp_meta_tool_server name="GitHub" identifier="github-mcp">`) {
		t.Fatal("compiled prompt missing GitHub mcp server entry")
	}
	if !strings.Contains(prompt, `<mcp_tool name="create_issue">`) {
		t.Fatal("compiled prompt missing create_issue tool")
	}
	if !strings.Contains(prompt, "<description>Creates a new GitHub issue</description>") {
		t.Fatal("compiled prompt missing tool description")
	}
	if !strings.Contains(prompt, "<input_schema>{&quot;type&quot;:&quot;object&quot;,&quot;properties&quot;:{&quot;title&quot;:{&quot;type&quot;:&quot;string&quot;}}}</input_schema>") {
		t.Fatal("compiled prompt missing tool input_schema")
	}
	// Check fallback description for fast_context_search
	if !strings.Contains(prompt, `<mcp_tool name="fast_context_search">`) {
		t.Fatal("compiled prompt missing fast_context_search")
	}
	if !strings.Contains(prompt, "Fast semantic codebase search and context discovery") {
		t.Fatal("compiled prompt missing fallback description for fast_context_search")
	}
}

func TestCompileInjectsCustomSubagentsSection(t *testing.T) {
	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			CustomSubagents: []*agentv1.CustomSubagent{
				{
					Name:        "tester",
					Description: "Automated test generator and runner",
				},
			},
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

	if !strings.Contains(prompt, "<subagents>") {
		t.Fatal("compiled prompt does not include <subagents>")
	}
	if !strings.Contains(prompt, `<subagent name="tester">Automated test generator and runner</subagent>`) {
		t.Fatal("compiled prompt missing custom subagent entry")
	}
}

func TestMergeLocalSkillsAndSubagents(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create a dummy skill
	skillDir := filepath.Join(tempDir, ".cursor", "skills", "ak-test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillContent := `---
name: ak-test-skill
description: "Test skill for automated verification"
---
# Test Skill Body
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	// 2. Create a dummy subagent
	agentsDir := filepath.Join(tempDir, ".cursor", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	agentContent := `---
name: dummy-agent
description: "A dummy agent for testing"
---
You are a dummy agent.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "dummy-agent.md"), []byte(agentContent), 0644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			Env: &agentv1.RequestContextEnv{
				WorkspacePaths: []string{tempDir},
			},
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

	if !strings.Contains(prompt, "<agent_skills>") {
		t.Fatal("compiled prompt missing <agent_skills>")
	}
	if !strings.Contains(prompt, "Test skill for automated verification") {
		t.Fatal("compiled prompt missing ak-test-skill description")
	}
	if !strings.Contains(prompt, "<subagents>") {
		t.Fatal("compiled prompt missing <subagents>")
	}
	if !strings.Contains(prompt, `<subagent name="dummy-agent">A dummy agent for testing</subagent>`) {
		t.Fatal("compiled prompt missing dummy-agent entry")
	}
}

func TestHydrateLocalMCPDescriptorsFullDescription(t *testing.T) {
	tempDir := t.TempDir()
	toolDefPath := filepath.Join(tempDir, "InspectChanges.json")
	toolJSON := `{
		"name": "InspectChanges",
		"description": "Inspects uncommitted Git changes (status and diff) with structured summaries."
	}`
	if err := os.WriteFile(toolDefPath, []byte(toolJSON), 0644); err != nil {
		t.Fatalf("write tool json: %v", err)
	}

	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			McpFileSystemOptions: &agentv1.McpFileSystemOptions{
				Enabled:             true,
				WorkspaceProjectDir: tempDir,
				McpDescriptors: []*agentv1.McpDescriptor{
					{
						ServerIdentifier: "user-code-intel",
						ServerName:       "code-intel",
						FolderPath:       proto.String(tempDir),
						Tools: []*agentv1.McpToolDescriptor{
							{
								ToolName:       "InspectChanges",
								DefinitionPath: proto.String(toolDefPath),
							},
						},
					},
					{
						ServerIdentifier: "user-fast-context",
						ServerName:       "fast-context",
						Tools: []*agentv1.McpToolDescriptor{
							{
								ToolName: "fast_context_search",
							},
						},
					},
				},
			},
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

	if !strings.Contains(prompt, "<mcp_file_system>") {
		t.Fatal("compiled prompt missing <mcp_file_system>")
	}
	if !strings.Contains(prompt, `<mcp_tool name="InspectChanges">`) {
		t.Fatal("compiled prompt missing InspectChanges tool")
	}
	if !strings.Contains(prompt, "<description>Inspects uncommitted Git changes (status and diff) with structured summaries.</description>") {
		t.Fatal("compiled prompt missing hydrated full description for InspectChanges")
	}
	if !strings.Contains(prompt, `<mcp_tool name="fast_context_search">`) {
		t.Fatal("compiled prompt missing fast_context_search tool")
	}
	if !strings.Contains(prompt, "Fast semantic codebase search and context discovery") {
		t.Fatal("compiled prompt missing fallback description for fast_context_search in mcp_file_system")
	}
}


