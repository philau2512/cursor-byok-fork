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

func TestExtractUserMessage_WithPrependUserMessages(t *testing.T) {
	message := &agentv1.AgentClientMessage{
		Message: &agentv1.AgentClientMessage_RunRequest{
			RunRequest: &agentv1.AgentRunRequest{
				Action: &agentv1.ConversationAction{
					Action: &agentv1.ConversationAction_UserMessageAction{
						UserMessageAction: &agentv1.UserMessageAction{
							PrependUserMessages: []*agentv1.UserMessage{
								{MessageId: "msg-old", Text: "old prompt"},
								{MessageId: "msg-probe", Text: "queued probe"},
							},
						},
					},
				},
			},
		},
	}

	userMessage := extractUserMessage(message)
	if userMessage == nil {
		t.Fatal("expected extractUserMessage to extract target user message from PrependUserMessages")
	}
	if userMessage.GetText() != "queued probe" {
		t.Fatalf("expected last prepend message 'queued probe', got %q", userMessage.GetText())
	}
	prepends := extractPrependUserMessages(message)
	if len(prepends) != 2 {
		t.Fatalf("expected 2 prepend messages, got %d", len(prepends))
	}
}

func TestFilterUnprocessedUserMessages(t *testing.T) {
	conv := &ConversationFile{
		Entries: []HistoryEntry{
			testUserMessageEntry(t, 1, "req-1", "old prompt"),
		},
	}
	// Give the entry a known MessageID
	conv.Entries[0].Payload = []byte(`{"text":"old prompt","message_id":"msg-old"}`)

	intent := InboundIntent{
		UserMessage: &agentv1.UserMessage{MessageId: "msg-old", Text: "old prompt"},
		PrependUserMessages: []*agentv1.UserMessage{
			{MessageId: "msg-old", Text: "old prompt"},
			{MessageId: "msg-probe", Text: "probe"},
		},
	}

	targetUser, remainingPrepends, hasNewWork := filterUnprocessedUserMessages(intent, conv)
	if !hasNewWork {
		t.Fatal("expected hasNewWork to be true for new probe message")
	}
	if targetUser == nil || targetUser.GetMessageId() != "msg-probe" {
		t.Fatalf("expected targetUser to be msg-probe, got %#v", targetUser)
	}
	if len(remainingPrepends) != 0 {
		t.Fatalf("expected remaining prepends to be 0 (since msg-old was filtered), got %d", len(remainingPrepends))
	}

	// Test when all messages already exist
	conv.Entries = append(conv.Entries, HistoryEntry{
		TurnSeq: 2,
		Kind:    "user_message",
		Payload: []byte(`{"text":"probe","message_id":"msg-probe"}`),
	})
	_, _, allExistWork := filterUnprocessedUserMessages(intent, conv)
	if allExistWork {
		t.Fatal("expected allExistWork to be false when all messages exist in history")
	}
}

