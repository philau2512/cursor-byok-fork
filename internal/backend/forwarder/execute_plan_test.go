package forwarder

import (
	"strings"
	"testing"

	"cursor/gen/agentv1"
)

func TestExtractConversationActionUserMessage_ExecutePlanAction(t *testing.T) {
	action := &agentv1.ConversationAction{
		Action: &agentv1.ConversationAction_ExecutePlanAction{
			ExecutePlanAction: &agentv1.ExecutePlanAction{},
		},
	}

	message := extractConversationActionUserMessage(action)
	if message == nil {
		t.Fatal("expected ExecutePlanAction to produce a user message")
	}
	if !strings.Contains(strings.ToLower(message.GetText()), "execute") {
		t.Fatalf("expected execution directive, got %q", message.GetText())
	}
}

func TestExtractUserMessage_ExecutePlanAction(t *testing.T) {
	message := &agentv1.AgentClientMessage{
		Message: &agentv1.AgentClientMessage_RunRequest{
			RunRequest: &agentv1.AgentRunRequest{
				Action: &agentv1.ConversationAction{
					Action: &agentv1.ConversationAction_ExecutePlanAction{
						ExecutePlanAction: &agentv1.ExecutePlanAction{},
					},
				},
			},
		},
	}

	userMessage := extractUserMessage(message)
	if userMessage == nil {
		t.Fatal("expected ExecutePlanAction to produce a user message")
	}
	if !strings.Contains(strings.ToLower(userMessage.GetText()), "execute") {
		t.Fatalf("expected execution directive, got %q", userMessage.GetText())
	}
}
