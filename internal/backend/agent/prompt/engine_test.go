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
		"description": "Inspects uncommitted Git changes (status and diff) with structured summaries.",
		"arguments": {
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Path to inspect"
				}
			},
			"required": ["path"]
		}
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
	if !strings.Contains(prompt, `<input_schema>{&quot;properties&quot;:{&quot;path&quot;:{&quot;description&quot;:&quot;Path to inspect&quot;,&quot;type&quot;:&quot;string&quot;}},&quot;required&quot;:[&quot;path&quot;],&quot;type&quot;:&quot;object&quot;}</input_schema>`) {
		t.Fatal("compiled prompt missing hydrated input_schema for InspectChanges")
	}
	if !strings.Contains(prompt, `<mcp_tool name="fast_context_search">`) {
		t.Fatal("compiled prompt missing fast_context_search tool")
	}
	if !strings.Contains(prompt, "Fast semantic codebase search and context discovery") {
		t.Fatal("compiled prompt missing fallback description for fast_context_search in mcp_file_system")
	}
}

func TestHydrateLocalMCPDescriptorsPreexistingDescriptionWithMissingSchema(t *testing.T) {
	tempDir := t.TempDir()
	toolDefPath := filepath.Join(tempDir, "query.json")
	toolJSON := `{
		"name": "query",
		"description": "File description",
		"arguments": {
			"type": "object",
			"properties": {
				"sql": {
					"type": "string"
				}
			}
		}
	}`
	if err := os.WriteFile(toolDefPath, []byte(toolJSON), 0644); err != nil {
		t.Fatalf("write tool json: %v", err)
	}

	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			McpMetaToolOptions: &agentv1.McpMetaToolOptions{
				Enabled: true,
				McpDescriptors: []*agentv1.McpDescriptor{
					{
						ServerIdentifier: "user-gridex",
						ServerName:       "gridex",
						Tools: []*agentv1.McpToolDescriptor{
							{
								ToolName:       "query",
								Description:    proto.String("Pre-existing client description"),
								DefinitionPath: proto.String(toolDefPath),
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

	if !strings.Contains(prompt, "<description>Pre-existing client description</description>") {
		t.Fatal("compiled prompt should preserve pre-existing client description")
	}
	if !strings.Contains(prompt, `<input_schema>{&quot;properties&quot;:{&quot;sql&quot;:{&quot;type&quot;:&quot;string&quot;}},&quot;type&quot;:&quot;object&quot;}</input_schema>`) {
		t.Fatal("compiled prompt missing hydrated input_schema when description already existed")
	}
}

func TestMergeLocalAgentRulesMultiLevel(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Root AGENTS.md
	rootRulePath := filepath.Join(tempDir, "AGENTS.md")
	if err := os.WriteFile(rootRulePath, []byte("Root project instructions: Always follow TDD."), 0644); err != nil {
		t.Fatalf("write root AGENTS.md: %v", err)
	}

	// 2. Nested level 2: packages/auth/AGENTS.md with frontmatter
	authDir := filepath.Join(tempDir, "packages", "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		t.Fatalf("mkdir authDir: %v", err)
	}
	authRulePath := filepath.Join(authDir, "AGENTS.md")
	authContent := "---\ndescription: Auth module rules\n---\nAuth instructions: Use RS256 for JWT tokens."
	if err := os.WriteFile(authRulePath, []byte(authContent), 0644); err != nil {
		t.Fatalf("write auth AGENTS.md: %v", err)
	}

	// 3. Nested level 4: services/core/handler/v1/agent.md (case-insensitive)
	handlerDir := filepath.Join(tempDir, "services", "core", "handler", "v1")
	if err := os.MkdirAll(handlerDir, 0755); err != nil {
		t.Fatalf("mkdir handlerDir: %v", err)
	}
	handlerRulePath := filepath.Join(handlerDir, "agent.md")
	if err := os.WriteFile(handlerRulePath, []byte("Handler instructions: Validate all request inputs."), 0644); err != nil {
		t.Fatalf("write handler agent.md: %v", err)
	}

	// 4. Ignored directories: node_modules and .git
	nodeModulesDir := filepath.Join(tempDir, "node_modules", "somepkg")
	if err := os.MkdirAll(nodeModulesDir, 0755); err != nil {
		t.Fatalf("mkdir nodeModulesDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nodeModulesDir, "AGENTS.md"), []byte("Should be ignored"), 0644); err != nil {
		t.Fatalf("write node_modules AGENTS.md: %v", err)
	}

	gitDir := filepath.Join(tempDir, ".git", "hooks")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("mkdir gitDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "AGENTS.md"), []byte("Should be ignored"), 0644); err != nil {
		t.Fatalf("write .git AGENTS.md: %v", err)
	}

	// 5. Exceeds max depth (> 5): a/b/c/d/e/f/AGENTS.md
	deepDir := filepath.Join(tempDir, "l1", "l2", "l3", "l4", "l5", "l6")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("mkdir deepDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "AGENTS.md"), []byte("Should be skipped due to depth"), 0644); err != nil {
		t.Fatalf("write deep AGENTS.md: %v", err)
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

	// Verify rules section presence
	if !strings.Contains(prompt, "<rules>") || !strings.Contains(prompt, "<user_rules") {
		t.Fatal("compiled prompt missing <rules> or <user_rules>")
	}

	// Verify root AGENTS.md
	if !strings.Contains(prompt, "# Project Rule: AGENTS.md (Root Workspace)") {
		t.Fatal("compiled prompt missing Root Workspace header")
	}
	if !strings.Contains(prompt, "Root project instructions: Always follow TDD.") {
		t.Fatal("compiled prompt missing root rule content")
	}

	// Verify packages/auth/AGENTS.md scope and content (frontmatter stripped)
	if !strings.Contains(prompt, "# Project Rule: packages/auth/AGENTS.md (Scope: packages/auth/)") {
		t.Fatal("compiled prompt missing packages/auth scope header")
	}
	if !strings.Contains(prompt, "Auth instructions: Use RS256 for JWT tokens.") {
		t.Fatal("compiled prompt missing auth rule content")
	}
	if strings.Contains(prompt, "description: Auth module rules") {
		t.Fatal("compiled prompt should strip frontmatter yaml block")
	}

	// Verify services/core/handler/v1/agent.md scope and content
	if !strings.Contains(prompt, "# Project Rule: services/core/handler/v1/agent.md (Scope: services/core/handler/v1/)") {
		t.Fatal("compiled prompt missing services/core/handler/v1 scope header")
	}
	if !strings.Contains(prompt, "Handler instructions: Validate all request inputs.") {
		t.Fatal("compiled prompt missing handler rule content")
	}

	// Verify ignored directories are not loaded
	if strings.Contains(prompt, "Should be ignored") {
		t.Fatal("compiled prompt included ignored directory AGENTS.md")
	}

	// Verify deep directory > 5 is skipped
	if strings.Contains(prompt, "Should be skipped due to depth") {
		t.Fatal("compiled prompt included rule exceeding max scan depth")
	}

	// Verify ordering: root should appear before packages/auth, and packages/auth before services/core/handler
	rootIdx := strings.Index(prompt, "# Project Rule: AGENTS.md (Root Workspace)")
	authIdx := strings.Index(prompt, "# Project Rule: packages/auth/AGENTS.md (Scope: packages/auth/)")
	handlerIdx := strings.Index(prompt, "# Project Rule: services/core/handler/v1/agent.md (Scope: services/core/handler/v1/)")

	if rootIdx > authIdx || authIdx > handlerIdx {
		t.Fatalf("rules ordering incorrect: rootIdx=%d, authIdx=%d, handlerIdx=%d", rootIdx, authIdx, handlerIdx)
	}
}

func TestMergeLocalAgentRulesIgnoresCursorInternalProjectFolderAndHome(t *testing.T) {
	tempWorkspace := t.TempDir()
	workspaceRulePath := filepath.Join(tempWorkspace, "AGENTS.md")
	if err := os.WriteFile(workspaceRulePath, []byte("Workspace rule: valid"), 0644); err != nil {
		t.Fatalf("write workspace rule: %v", err)
	}

	tempHome := t.TempDir()
	fakeCursorProjectFolder := filepath.Join(tempHome, ".cursor", "projects", "d-Admin-Documents-PROJECTS-test")
	if err := os.MkdirAll(fakeCursorProjectFolder, 0755); err != nil {
		t.Fatalf("mkdir fakeCursorProjectFolder: %v", err)
	}

	// Home should not be scanned
	homeRulePath := filepath.Join(tempHome, "AGENTS.md")
	if err := os.WriteFile(homeRulePath, []byte("Home rule: should be ignored"), 0644); err != nil {
		t.Fatalf("write home rule: %v", err)
	}

	// AppData inside home should definitely not be scanned
	appDataDir := filepath.Join(tempHome, "AppData", "Local", "firefox-manager")
	if err := os.MkdirAll(appDataDir, 0755); err != nil {
		t.Fatalf("mkdir appdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDataDir, "AGENTS.md"), []byte("AppData rule: should be ignored"), 0644); err != nil {
		t.Fatalf("write appdata rule: %v", err)
	}

	compiled, err := NewEngine().Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			Env: &agentv1.RequestContextEnv{
				WorkspacePaths: []string{tempWorkspace},
				ProjectFolder:  fakeCursorProjectFolder,
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

	if !strings.Contains(prompt, "Workspace rule: valid") {
		t.Fatal("expected workspace rule to be included")
	}
	if strings.Contains(prompt, "Home rule: should be ignored") {
		t.Fatal("home rule should not be included")
	}
	if strings.Contains(prompt, "AppData rule: should be ignored") {
		t.Fatal("AppData rule should not be included")
	}
}

func TestMergeLocalAgentRulesCacheOnlyOnNewUserMessage(t *testing.T) {
	tempWorkspace := t.TempDir()
	workspaceRulePath := filepath.Join(tempWorkspace, "AGENTS.md")
	if err := os.WriteFile(workspaceRulePath, []byte("Version 1 rule"), 0644); err != nil {
		t.Fatalf("write workspace rule: %v", err)
	}

	engine := NewEngine()

	// 1. First call with new user message: scans disk and caches Version 1
	compiled1, err := engine.Compile(CompileInput{
		Mode:                   agentv1.AgentMode_AGENT_MODE_AGENT,
		CurrentUserMessageText: "Hello agent",
		RequestContext: &agentv1.RequestContext{
			Env: &agentv1.RequestContextEnv{
				WorkspacePaths: []string{tempWorkspace},
			},
		},
	})
	if err != nil {
		t.Fatalf("Compile 1: %v", err)
	}
	if !strings.Contains(compiled1.Messages[len(compiled1.Messages)-2].Content, "Version 1 rule") {
		t.Fatal("expected Version 1 rule in compile 1")
	}

	// 2. Modify rule on disk mid-turn (simulating changes while tool calls are running)
	if err := os.WriteFile(workspaceRulePath, []byte("Version 2 rule"), 0644); err != nil {
		t.Fatalf("update workspace rule: %v", err)
	}

	// 3. Second call during tool call (CurrentUserMessageText is empty): should use cache, NOT re-scan
	compiled2, err := engine.Compile(CompileInput{
		Mode: agentv1.AgentMode_AGENT_MODE_AGENT,
		RequestContext: &agentv1.RequestContext{
			Env: &agentv1.RequestContextEnv{
				WorkspacePaths: []string{tempWorkspace},
			},
		},
	})
	if err != nil {
		t.Fatalf("Compile 2: %v", err)
	}
	var c2Text string
	for _, m := range compiled2.Messages {
		c2Text += m.Content + "\n"
	}
	if strings.Contains(c2Text, "Version 2 rule") {
		t.Fatal("tool call step should have used cached rules and not re-scanned disk")
	}
	if !strings.Contains(c2Text, "Version 1 rule") {
		t.Fatal("tool call step should still contain cached Version 1 rule")
	}

	// 4. Third call when user sends a new message: cache is invalidated, picks up Version 2
	compiled3, err := engine.Compile(CompileInput{
		Mode:                   agentv1.AgentMode_AGENT_MODE_AGENT,
		CurrentUserMessageText: "Next user turn",
		RequestContext: &agentv1.RequestContext{
			Env: &agentv1.RequestContextEnv{
				WorkspacePaths: []string{tempWorkspace},
			},
		},
	})
	if err != nil {
		t.Fatalf("Compile 3: %v", err)
	}
	var c3Text string
	for _, m := range compiled3.Messages {
		c3Text += m.Content + "\n"
	}
	if !strings.Contains(c3Text, "Version 2 rule") {
		t.Fatal("new user message should invalidate cache and pick up Version 2 rule")
	}
}
