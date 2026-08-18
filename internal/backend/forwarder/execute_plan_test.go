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

func TestShouldIgnoreEmptyResumeRunRequestWhenConversationIsActive(t *testing.T) {
	broker := NewStreamBroker()
	service := &Service{broker: broker}
	if _, err := broker.OpenStream(
		"active-request", "conversation-1", 1, "default", "default",
		agentv1.AgentMode_AGENT_MODE_AGENT, "working",
	); err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}

	conversationID := "conversation-1"
	runRequest := &agentv1.AgentRunRequest{
		ConversationId: &conversationID,
		Action: &agentv1.ConversationAction{
			Action: &agentv1.ConversationAction_ResumeAction{
				ResumeAction: &agentv1.ResumeAction{},
			},
		},
	}

	if !service.shouldIgnoreEmptyResumeRunRequest("reconnect-request", runRequest, nil, nil) {
		t.Fatal("expected payload-free resume to be ignored while its conversation is active")
	}
}
