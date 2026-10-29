package forwarder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cursor/gen/agentv1"
	promptengine "cursor/internal/backend/agent/prompt"
)

func TestExtractRequestContextFromDynamicContextAndLegacy(t *testing.T) {
	legacyCtx := &agentv1.RequestContext{
		Rules: []*agentv1.CursorRule{
			{FullPath: "/rules/legacy_rule.md"},
		},
	}
	dynamicCtx := &agentv1.RequestContext{
		Env: &agentv1.RequestContextEnv{
			ProjectFolder: "/test/project",
		},
	}

	msg := &agentv1.AgentClientMessage{
		Message: &agentv1.AgentClientMessage_RunRequest{
			RunRequest: &agentv1.AgentRunRequest{
				Action: &agentv1.ConversationAction{
					RequestContextParts: &agentv1.RequestContextPartReferences{
						DynamicContext: dynamicCtx,
					},
					Action: &agentv1.ConversationAction_UserMessageAction{
						UserMessageAction: &agentv1.UserMessageAction{
							RequestContext: legacyCtx,
						},
					},
				},
			},
		},
	}

	extracted := extractRequestContext(msg)
	if extracted == nil {
		t.Fatal("expected non-nil extracted request context")
	}
	if extracted.GetEnv() == nil || extracted.GetEnv().GetProjectFolder() != "/test/project" {
		t.Errorf("expected project folder from dynamic context, got %#v", extracted.GetEnv())
	}
	if len(extracted.GetRules()) != 1 || extracted.GetRules()[0].GetFullPath() != "/rules/legacy_rule.md" {
		t.Errorf("expected rules merged from legacy context, got %#v", extracted.GetRules())
	}
}

func TestNormalizeRealtimeRequestContextPreservesEnvAndRepos(t *testing.T) {
	rc := &agentv1.RequestContext{
		Env: &agentv1.RequestContextEnv{
			ProjectFolder: "/workspace/root",
			WorkspacePaths: []string{"/workspace/root"},
		},
		GitRepos: []*agentv1.GitRepoInfo{
			{Path: "/workspace/root"},
		},
		Rules: []*agentv1.CursorRule{
			{FullPath: "/rules/user_rule.md"},
		},
	}

	normalized := normalizeRealtimeRequestContextForStorage(rc)
	if normalized == nil {
		t.Fatal("expected non-nil normalized realtime request context")
	}
	if normalized.GetEnv() == nil || normalized.GetEnv().GetProjectFolder() != "/workspace/root" {
		t.Errorf("expected Env to be preserved in realtime context, got %#v", normalized.GetEnv())
	}
	if len(normalized.GetGitRepos()) != 1 || normalized.GetGitRepos()[0].GetPath() != "/workspace/root" {
		t.Errorf("expected GitRepos to be preserved in realtime context, got %#v", normalized.GetGitRepos())
	}
}

func TestLocalDiscoveryWithParentMarker(t *testing.T) {
	tempDir := t.TempDir()
	workspaceDir := filepath.Join(tempDir, "workspace")
	subprojectDir := filepath.Join(workspaceDir, "subproject")
	skillsDir := filepath.Join(workspaceDir, ".cursor", "skills", "test-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subprojectDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: test-skill\ndescription: Test skill description\n---\n# Test Body"
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	rc := &agentv1.RequestContext{
		Env: &agentv1.RequestContextEnv{
			WorkspacePaths: []string{subprojectDir},
		},
	}

	messages := promptengine.BuildRequestContextReplayMessages(rc)
	if len(messages) == 0 {
		t.Fatal("expected replay messages to be generated")
	}
	foundSkill := false
	for _, m := range messages {
		if strings.Contains(m.Content, "Test skill description") {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Error("expected test-skill from parent workspace to be discovered via candidate roots")
	}
}