package forwarder

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"cursor/gen/aiserverv1"
	modeladapter "cursor/internal/backend/agent/model"
)

type mockCommitMessageProvider struct {
	sinkEvents []modeladapter.ModelEvent
	err        error
	lastReq    ProviderRequest
}

func (m *mockCommitMessageProvider) StartStream(_ context.Context, req ProviderRequest, sink func(modeladapter.ModelEvent) error) error {
	m.lastReq = req
	for _, event := range m.sinkEvents {
		if err := sink(event); err != nil {
			return err
		}
	}
	return m.err
}

func TestWriteGitCommitMessageHappyPath(t *testing.T) {
	provider := &mockCommitMessageProvider{
		sinkEvents: []modeladapter.ModelEvent{
			{Kind: modeladapter.ModelEventKindTextDelta, Text: "feat: add commit message generator\n\n- support local generation"},
		},
	}
	service := &Service{
		provider: provider,
	}

	req := connect.NewRequest(&aiserverv1.WriteGitCommitMessageRequest{
		Diffs: []string{"diff --git a/main.go b/main.go\n+ func main() {}"},
	})

	resp, err := service.WriteGitCommitMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteGitCommitMessage unexpected error: %v", err)
	}
	if resp == nil || resp.Msg == nil {
		t.Fatal("expected non-nil response")
	}
	expected := "feat: add commit message generator\n\n- support local generation"
	if resp.Msg.GetCommitMessage() != expected {
		t.Fatalf("commit message = %q, want %q", resp.Msg.GetCommitMessage(), expected)
	}
}

func TestWriteGitCommitMessageStripsPrefixAndFences(t *testing.T) {
	provider := &mockCommitMessageProvider{
		sinkEvents: []modeladapter.ModelEvent{
			{Kind: modeladapter.ModelEventKindTextDelta, Text: "```\nCommit Message: fix(auth): resolve token issue\n```"},
		},
	}
	service := &Service{
		provider: provider,
	}

	req := connect.NewRequest(&aiserverv1.WriteGitCommitMessageRequest{
		Diffs: []string{"diff content"},
	})

	resp, err := service.WriteGitCommitMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteGitCommitMessage unexpected error: %v", err)
	}
	expected := "fix(auth): resolve token issue"
	if resp.Msg.GetCommitMessage() != expected {
		t.Fatalf("commit message = %q, want %q", resp.Msg.GetCommitMessage(), expected)
	}
}

func TestWriteGitCommitMessageFlattensWrappedSubject(t *testing.T) {
	provider := &mockCommitMessageProvider{
		sinkEvents: []modeladapter.ModelEvent{
			{Kind: modeladapter.ModelEventKindTextDelta, Text: "chore: remove WriteGitCommitMessage\nroute"},
		},
	}
	service := &Service{
		provider: provider,
	}

	req := connect.NewRequest(&aiserverv1.WriteGitCommitMessageRequest{
		Diffs: []string{"diff content"},
	})

	resp, err := service.WriteGitCommitMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteGitCommitMessage unexpected error: %v", err)
	}
	expected := "chore: remove WriteGitCommitMessage route"
	if resp.Msg.GetCommitMessage() != expected {
		t.Fatalf("commit message = %q, want %q", resp.Msg.GetCommitMessage(), expected)
	}
}

func TestWriteGitCommitMessageRejectsEmptyDiffs(t *testing.T) {
	service := &Service{
		provider: &mockCommitMessageProvider{},
	}

	req := connect.NewRequest(&aiserverv1.WriteGitCommitMessageRequest{
		Diffs: []string{},
	})

	_, err := service.WriteGitCommitMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty diffs, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected code InvalidArgument, got %v", connect.CodeOf(err))
	}
}

func TestWriteGitCommitMessageRejectsToolInvocation(t *testing.T) {
	provider := &mockCommitMessageProvider{
		sinkEvents: []modeladapter.ModelEvent{
			{Kind: modeladapter.ModelEventKindToolLikeCompleted},
		},
	}
	service := &Service{
		provider: provider,
	}

	req := connect.NewRequest(&aiserverv1.WriteGitCommitMessageRequest{
		Diffs: []string{"diff content"},
	})

	_, err := service.WriteGitCommitMessage(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for tool invocation, got nil")
	}
	if !strings.Contains(err.Error(), "must not invoke tools") {
		t.Fatalf("unexpected error message: %v", err)
	}
}